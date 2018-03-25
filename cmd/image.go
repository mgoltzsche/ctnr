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
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	humanize "github.com/dustin/go-humanize"
	"github.com/mgoltzsche/cntnr/image/builder"
	"github.com/opencontainers/go-digest"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

var (
	imageCmd = &cobra.Command{
		Use:   "image",
		Short: "Manages images",
		Long:  `Provides subcommands to manage images in the local store.`,
	}
	imageListCmd = &cobra.Command{
		Use:   "list",
		Short: "Lists all images",
		Long:  `Lists all images available in the local store.`,
		Run:   wrapRun(runImageList),
	}
	imageTagCmd = &cobra.Command{
		Use:   "tag IMAGE...",
		Short: "Tags one or many images",
		Long:  `Tags one or many images in the local store.`,
		Run:   wrapRun(runImageTag),
	}
	imageUntagCmd = &cobra.Command{
		Use:   "untag IMAGE...",
		Short: "Untags one or many images",
		Long:  `Untags one or many images in the local store.`,
		Run:   wrapRun(runImageUntag),
	}
	imageRmCmd = &cobra.Command{
		Use:   "rm IMAGEID",
		Short: "Removes the image from the store",
		Long:  `Removes the image as well as all tags referencing it from the store.`,
		Run:   wrapRun(runImageRm),
	}
	imageGcCmd = &cobra.Command{
		Use:   "gc",
		Short: "Garbage collects image blobs",
		Long:  `Garbage collects all image blobs in the local store that are not referenced.`,
		Run:   wrapRun(runImageGc),
	}
	imageImportCmd = &cobra.Command{
		Use:   "import IMAGE",
		Short: "Imports an image",
		Long: `Fetches an image either from a local or remote source and 
imports it into the local store.`,
		Run: wrapRun(runImageImport),
	}
	imageExportCmd = &cobra.Command{
		Use:   "export IMAGEREF DEST",
		Short: "Exports an image",
		Long: `Exports an image from the local store 
to a local or remote destination.`,
		Run: func(cmd *cobra.Command, args []string) {
			panic("TODO: export image")
		},
	}
	imageCatConfigCmd = &cobra.Command{
		Use:   "cat-config IMAGE",
		Short: "Prints an image's configuration",
		Long:  `Prints an image's configuration.`,
		Run:   wrapRun(runImageCatConfig),
	}
	imageBuildCmd = &cobra.Command{
		Use:   "create",
		Short: "Builds a new image from the provided options",
		Long:  `Builds a new image from the provided options.`,
		Run:   wrapRun(runImageBuildRun),
	}
	flagImageTTL time.Duration
	flagImage    string
	imageBuilder *builder.ImageBuilder
)

func init() {
	imageBuilder = builder.NewImageBuilder()
	initImageBuildFlags(imageBuildCmd.Flags(), imageBuilder)
	imageCmd.AddCommand(imageListCmd)
	imageCmd.AddCommand(imageTagCmd)
	imageCmd.AddCommand(imageUntagCmd)
	imageCmd.AddCommand(imageRmCmd)
	imageCmd.AddCommand(imageGcCmd)
	imageCmd.AddCommand(imageImportCmd)
	imageCmd.AddCommand(imageExportCmd)
	imageCmd.AddCommand(imageCatConfigCmd)
	imageCmd.AddCommand(imageBuildCmd)
	imageGcCmd.Flags().DurationVarP(&flagImageTTL, "ttl", "t", time.Duration(1000*1000*1000*60*60*24*7 /*7 days*/), "image lifetime before it gets garbage collected")
}

func runImageList(cmd *cobra.Command, args []string) (err error) {
	imgs, err := store.Images()
	if err != nil {
		return
	}
	sort.Slice(imgs, func(i, j int) bool {
		return imgs[i].LastUsed.Before(imgs[j].LastUsed)
	})
	f := "%-35s %-15s  %-71s  %-15s  %8s\n"
	fmt.Printf(f, "REPO", "REF", "ID", "CREATED", "SIZE")
	for _, img := range imgs {
		fmt.Printf(f, img.Repo, img.Ref, img.ID(), humanize.Time(img.Created), humanize.Bytes(img.Size()))
	}
	return
}

func runImageGc(cmd *cobra.Command, args []string) error {
	if len(args) != 0 {
		return usageError("No argument expected: " + args[0])
	}
	return store.ImageGC(time.Now().Add(-flagImageTTL))
}

func runImageRm(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return usageError("No IMAGEID provided")
	}
	ids := make([]digest.Digest, len(args))
	for i, a := range args {
		if d, e := digest.Parse(a); e == nil && d.Validate() == nil {
			ids[i] = d
		} else {
			return errors.Errorf("invalid IMAGEID %q provided", a)
		}
	}
	return store.DelImage(ids...)
}

func runImageImport(cmd *cobra.Command, args []string) (err error) {
	if len(args) != 1 {
		return usageError("No image provided")
	}
	lockedStore, err := openImageStore()
	if err != nil {
		return
	}

	img, err := lockedStore.ImportImage(args[0])
	if err == nil {
		fmt.Fprintln(os.Stdout, img.ID())
	}
	return
}

func runImageTag(cmd *cobra.Command, args []string) (err error) {
	if len(args) < 2 {
		return usageError("ImageID and tag arguments required")
	}
	lockedStore, err := openImageStore()
	if err != nil {
		return
	}

	imageId, err := digest.Parse(args[0])
	if err != nil {
		return
	}
	for _, tag := range args[1:] {
		if _, err = lockedStore.TagImage(imageId, tag); err != nil {
			return
		}
	}
	return
}

func runImageUntag(cmd *cobra.Command, args []string) (err error) {
	if len(args) == 0 {
		return usageError("No image tag argument provided")
	}
	lockedStore, err := openImageStore()
	if err != nil {
		return
	}

	for _, tag := range args {
		e := lockedStore.UntagImage(tag)
		if e != nil {
			loggers.Error.Println(e)
			err = e
		}
	}
	if err != nil {
		err = errors.New("Failed to untag all images")
	}
	return
}

func runImageCatConfig(cmd *cobra.Command, args []string) (err error) {
	if len(args) != 1 {
		return usageError("No IMAGE argument provided")
	}
	lockedStore, err := openImageStore()
	if err != nil {
		return
	}

	img, err := lockedStore.ImageByName(args[0])
	if err != nil {
		return
	}
	cfg, err := img.Config()
	if err != nil {
		return
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return
	}
	fmt.Println(string(b))
	return
}

func runImageBuildRun(cmd *cobra.Command, args []string) (err error) {
	if len(args) != 0 {
		return usageError(fmt.Sprintf("No arguments supported but %q provided", args[0]))
	}
	lockedStore, err := openImageStore()
	if err != nil {
		return
	}

	cache := builder.NewNoOpCache()
	if !flagNoCache {
		cache = builder.NewImageBuildCache(filepath.Join(flagStoreDir, "image-build-cache"), loggers.Warn)
	}
	proot := ""
	if flagProot {
		proot = flagPRootPath
		if proot == "" {
			return usageError("proot enabled but no --proot-path provided")
		}
	}
	tmpDir := filepath.Join(flagStoreDir, "tmp")
	img, err := imageBuilder.Build(builder.ImageBuildConfig{
		Images:   lockedStore,
		Bundles:  store.BundleStore,
		Cache:    cache,
		Tempfs:   tmpDir,
		Rootless: flagRootless,
		PRoot:    proot,
		Loggers:  loggers,
	})
	if err == nil {
		fmt.Fprintln(os.Stdout, img.ID())
	}
	return
}
