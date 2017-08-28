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
	"github.com/spf13/cobra"
)

var (
	imageCmd = &cobra.Command{
		Use:   "image",
		Short: "Manages images",
		Long:  `This subcommand operates on image(s) in the local store.`,
		/*Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("Hugo Static Site Generator v0.9 -- HEAD")
		},*/
	}
	imageListCmd = &cobra.Command{
		Use:   "ls",
		Short: "Lists all images available in the local store",
		Long:  `Lists all images available in the local store.`,
		Run: func(cmd *cobra.Command, args []string) {
			panic("TODO: List images")
		},
	}
	imageRemoveCmd = &cobra.Command{
		Use:   "rm",
		Short: "Removes an image from the local store",
		Long:  `Removes an image from the local store.`,
		Run: func(cmd *cobra.Command, args []string) {
			panic("TODO: Remove image")
		},
	}
	imageImportCmd = &cobra.Command{
		Use:   "import",
		Short: "Imports an image into the local store",
		Long: `Fetches an image either from a local or remote source and 
imports it into the local store.`,
		Run: func(cmd *cobra.Command, args []string) {
			panic("TODO: Import image")
		},
	}
	imageExportCmd = &cobra.Command{
		Use:   "export",
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
	imageCmd.AddCommand(imageRemoveCmd)
	imageCmd.AddCommand(imageImportCmd)
	imageCmd.AddCommand(imageExportCmd)
	// TODO: image build
}
