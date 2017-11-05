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

	humanize "github.com/dustin/go-humanize"
	"github.com/mgoltzsche/cntnr/model"
	"github.com/spf13/cobra"
)

var (
	bundleCmd = &cobra.Command{
		Use:   "bundle",
		Short: "Manages OCI runtime bundles",
		Long:  `This subcommand operates on OCI runtime bundles.`,
	}
	bundleListCmd = &cobra.Command{
		Use:   "list",
		Short: "Lists all bundles available in the local store (--bundle-store-dir)",
		Long:  `Lists all bundles available in the local store (--bundle-store-dir).`,
		Run:   handleError(runBundleList),
	}
	bundleCreateCmd = &cobra.Command{
		Use:   "create [flags] IMAGE [COMMAND]",
		Short: "Creates a new bundle",
		Long:  `Creates a new OCI runtime bundle`,
		Run:   handleError(runBundleCreate),
	}
	bundleDeleteCmd = &cobra.Command{
		Use:   "delete BUNDLEID",
		Short: "Deletes a bundle from the local store",
		Long:  `Deletes a bundle from the local store.`,
		Run:   handleError(runBundleDelete),
	}
	// TODO: bundle run
)

func init() {
	bundleCmd.AddCommand(bundleListCmd)
	bundleCmd.AddCommand(bundleDeleteCmd)
	bundleCmd.AddCommand(bundleCreateCmd)
	initBundleFlags(bundleCreateCmd.Flags())
}

func runBundleList(cmd *cobra.Command, args []string) (err error) {
	l, err := store.Bundles()
	if err != nil {
		return
	}
	f := "%-26s  %-71s  %s\n"
	fmt.Printf(f, "ID", "IMAGE", "CREATED")
	for _, c := range l {
		img := "<none>"
		if c.Image() != nil {
			img = (*c.Image()).String()
		}
		fmt.Printf(f, c.ID(), img, humanize.Time(c.Created()))
	}
	return
}

func runBundleCreate(cmd *cobra.Command, args []string) (err error) {
	if err = flagsBundle.setBundleArgs(args); err != nil {
		return
	}
	panic("TODO: bundle dir option")
	bundleDir := ""
	istore, err := store.OpenLockedImageStore()
	if err != nil {
		return
	}
	defer istore.Close()
	c, err := createRuntimeBundle(istore, &model.Project{}, flagsBundle.last(), bundleDir)
	fmt.Println(c.Dir())
	return
}

func runBundleDelete(cmd *cobra.Command, args []string) (err error) {
	if len(args) == 0 {
		return usageError("No bundle specified to remove")
	}
	failed := false
	for _, id := range args {
		b, err := store.Bundle(id)
		if err == nil {
			bl, e := b.Lock()
			if e == nil {
				err = bl.Delete()
				if err == nil {
					continue
				}
			} else {
				err = e
			}
		}
		os.Stderr.WriteString(err.Error() + "\n")
		failed = true
	}
	if failed {
		err = fmt.Errorf("bundle rm: Not all specified bundles have been removed")
	}
	return
}
