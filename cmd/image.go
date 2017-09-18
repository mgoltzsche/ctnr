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
	"os"
	"strings"

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
		Use:   "delete IMAGE...",
		Short: "Deletes one or many image references from the local store",
		Long:  `Deletes an image reference from the local store.`,
		Run:   handleError(runImageDelete),
	}
	imageGcCmd = &cobra.Command{
		Use:   "gc IMAGE...",
		Short: "Garbage collects image blobs",
		Long:  `Garbage collects all image blobs in the local store that are not referenced.`,
		Run:   handleError(runImageGc),
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
	imageCmd.AddCommand(imageGcCmd)
	imageCmd.AddCommand(imageImportCmd)
	imageCmd.AddCommand(imageExportCmd)
	// TODO: image build
}

func runImageList(cmd *cobra.Command, args []string) (err error) {
	imgs, err := store.Images()
	if err != nil {
		return
	}
	f := "%-35s %-15s  %-71s  %-15s  %8s\n"
	fmt.Printf(f, "NAME", "REF", "ID", "CREATED", "SIZE")
	for _, img := range imgs {
		fmt.Printf(f, img.Name, img.Ref, img.ID, humanize.Time(img.Created), humanize.Bytes(img.Size))
	}
	return
}

func runImageGc(cmd *cobra.Command, args []string) error {
	if len(args) != 0 {
		return usageError("No argument expected: " + args[0])
	}
	return store.ImageGC()
}

func runImageImport(cmd *cobra.Command, args []string) error {
	if len(args) != 1 {
		return usageError("No image provided to import")
	}
	_, err := store.ImportImage(args[0])
	return err
}

func runImageDelete(cmd *cobra.Command, args []string) (err error) {
	if len(args) == 0 {
		return usageError("No image argument provided")
	}
	for _, arg := range args {
		refIdx := strings.LastIndex(arg, ":")
		name := arg
		ref := ""
		if refIdx > 0 && refIdx+1 < len(name) {
			name = arg[:refIdx]
			ref = arg[refIdx+1:]
		}
		e := store.DeleteImage(name, ref)
		if e != nil {
			os.Stderr.WriteString(fmt.Sprintf("Cannot delete image %q: %s", arg, e))
			err = e
		}
	}
	return
}
