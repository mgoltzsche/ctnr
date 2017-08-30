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
	humanize "github.com/dustin/go-humanize"
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
		Short: "Lists all images available in the local store",
		Long:  `Lists all images available in the local store.`,
		Run:   handleError(runImageList),
	}
	imageDeleteCmd = &cobra.Command{
		Use:   "delete IMAGE",
		Short: "Deletes an image reference from the local store",
		Long:  `Deletes an image reference from the local store.`,
		Run: func(cmd *cobra.Command, args []string) {
			panic("TODO: Remove image")
		},
	}
	imageImportCmd = &cobra.Command{
		Use:   "import IMAGE",
		Short: "Imports an image into the local store",
		Long: `Fetches an image either from a local or remote source and 
imports it into the local store.`,
		Run: handleError(runImageImport),
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
)

func init() {
	imageCmd.AddCommand(imageListCmd)
	imageCmd.AddCommand(imageDeleteCmd)
	imageCmd.AddCommand(imageImportCmd)
	imageCmd.AddCommand(imageExportCmd)
	// TODO: image build, gc
}

func runImageList(cmd *cobra.Command, args []string) error {
	imgs, err := imageMngr.List()
	if err != nil {
		return err
	}
	f := "%-35s  %-71s  %-12s  %8s\n"
	fmt.Printf(f, "REF", "DIGEST", "CREATED", "SIZE")
	for _, img := range imgs {
		size, err := img.Size()
		if err != nil {
			return err
		}
		fmt.Printf(f, img.Name(), img.Digest(), humanize.Time(img.Created()), humanize.Bytes(size))
	}
	return nil
}

func runImageImport(cmd *cobra.Command, args []string) error {
	if len(args) != 1 {
		return usageError("Image argument missing")
	}
	_, err := imageMngr.Image(args[0])
	return err
}
