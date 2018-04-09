// Copyright © 2017 Max Goltzsche
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/mgoltzsche/cntnr/image"
	"github.com/opencontainers/go-digest"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

var (
	commitCmd = &cobra.Command{
		Use:   "commit [flags] CONTAINER [IMAGENAME]",
		Short: "Creates a new image from the current container",
		Long:  `Creates a new image from the current container.`,
		Run:   wrapRun(runCommit),
	}
	flagAuthor  string
	flagComment string
)

func init() {
	commitCmd.Flags().StringVarP(&flagAuthor, "author", "a", "", "Sets the new layer's author")
	commitCmd.Flags().StringVarP(&flagComment, "comment", "c", "", "Sets the new layer's comment")
}

func runCommit(cmd *cobra.Command, args []string) (err error) {
	if len(args) < 1 || len(args) > 2 {
		return usageError("Invalid argument")
	}
	bundleId := args[0]
	b, err := store.Bundle(bundleId)
	if err != nil {
		return
	}
	lockedBundle, err := b.Lock()
	if err != nil {
		return
	}
	defer lockedBundle.Close()
	lockedStore, err := openImageStore()
	if err != nil {
		return
	}
	spec, err := lockedBundle.Spec()
	if err != nil {
		return
	}
	if spec.Root == nil {
		return errors.Errorf("bundle %q has no root path", bundleId)
	}
	rootfs := filepath.Join(b.Dir(), spec.Root.Path)
	src, err := lockedStore.NewLayerSource(rootfs, false)
	if err != nil {
		return
	}

	// Try to create new image
	var (
		imgId digest.Digest
		img   image.Image
	)
	if img, err = lockedStore.AddImageLayer(src, lockedBundle.Image(), flagAuthor, flagComment); err == nil {
		imgId = img.ID()
		err = lockedBundle.SetParentImageId(&imgId)
	} else if image.IsEmptyLayerDiff(err) {
		bImgId := lockedBundle.Image()
		if bImgId == nil {
			panic("bundle has no parent but provides no layer contents")
		}
		imgId = *bImgId
		err = nil
	}

	// Tag image
	if err == nil {
		if len(args) > 1 {
			for _, tag := range args[1:] {
				if _, err = lockedStore.TagImage(imgId, tag); err != nil {
					return
				}
			}
		}
		fmt.Println(imgId)
	}
	return
}
