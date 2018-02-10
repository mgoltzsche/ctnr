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
	img, err := lockedStore.AddImageLayer(filepath.Join(b.Dir(), spec.Root.Path), lockedBundle.Image(), flagAuthor, flagComment)
	if err != nil {
		// TODO: distinguish between nothing to commit and real failure
		return
	}
	imgId := img.ID()
	if err = lockedBundle.SetParentImageId(&imgId); err != nil {
		return
	}
	if len(args) > 1 {
		for _, tag := range args[1:] {
			if _, err = lockedStore.TagImage(img.ID(), tag); err != nil {
				return
			}
		}
	}
	fmt.Println(img.ID())
	return
}
