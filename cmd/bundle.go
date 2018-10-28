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
	"os/signal"
	"sort"
	"syscall"
	"time"

	humanize "github.com/dustin/go-humanize"
	"github.com/mgoltzsche/ctnr/bundle"
	"github.com/mgoltzsche/ctnr/run"
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
		Run:   wrapRun(runBundleList),
	}
	bundleCreateCmd = &cobra.Command{
		Use:   "create [flags] IMAGE [COMMAND]",
		Short: "Creates a new bundle",
		Long:  `Creates a new OCI runtime bundle`,
		Run:   wrapRun(runBundleCreate),
	}
	bundleDeleteCmd = &cobra.Command{
		Use:   "delete BUNDLEID",
		Short: "Deletes a bundle from the local store",
		Long:  `Deletes a bundle from the local store.`,
		Run:   wrapRun(runBundleDelete),
	}
	bundleRunCmd = &cobra.Command{
		Use:   "run [flags] BUNDLEID|BUNDLEDIR",
		Short: "Runs an existing bundle",
		Long:  `Runs an existing OCI runtime bundle`,
		Run:   wrapRun(runBundleRun),
	}
	bundleGcCmd = &cobra.Command{
		Use:   "gc",
		Short: "Garage collects all bundles in the bundle store",
		Long:  `Garage collects all bundles in the bundle store.`,
		Run:   wrapRun(runBundleGc),
	}
	flagBundleTTL    time.Duration
	defaultBundleTTL = time.Duration(1000 * 1000 * 1000 * 60 * 30) /*30min*/
)

func init() {
	bundleCmd.AddCommand(bundleListCmd)
	bundleCmd.AddCommand(bundleDeleteCmd)
	bundleCmd.AddCommand(bundleCreateCmd)
	bundleCmd.AddCommand(bundleRunCmd)
	bundleCmd.AddCommand(bundleGcCmd)
	flagsBundle.InitContainerFlags(bundleCreateCmd.Flags())
	flagsBundle.InitRunFlags(bundleRunCmd.Flags())
	bundleGcCmd.Flags().DurationVarP(&flagBundleTTL, "ttl", "t", defaultBundleTTL, "bundle lifetime before it gets garbage collected")
}

func runBundleList(cmd *cobra.Command, args []string) (err error) {
	l, err := store.Bundles()
	if err != nil {
		return
	}
	sort.Slice(l, func(i, j int) bool {
		return l[i].Created().Before(l[j].Created())
	})
	f := "%-26s  %-71s  %s\n"
	fmt.Printf(f, "ID", "IMAGE", "CREATED")
	for _, c := range l {
		imageId := c.Image()
		img := "<none>"
		if imageId != nil {
			img = imageId.String()
		}
		fmt.Printf(f, c.ID(), img, humanize.Time(c.Created()))
	}
	return
}

func runBundleCreate(cmd *cobra.Command, args []string) (err error) {
	if err = flagsBundle.SetBundleArgs(args); err != nil {
		return
	}
	service, err := flagsBundle.Read()
	if err != nil {
		return
	}
	c, err := createRuntimeBundle(service, resourceResolver("", nil))
	if err != nil {
		return
	}
	defer c.Close()
	return
}

func runBundleDelete(cmd *cobra.Command, args []string) (err error) {
	if len(args) == 0 {
		return usageError("No bundle specified to remove")
	}
	failedIds := []string{}
	for _, id := range args {
		b, e := store.Bundle(id)
		if e == nil {
			bl, e := b.Lock()
			if e == nil {
				e = bl.Delete()
				if e == nil {
					loggers.Info.WithField("id", id).Println("Bundle deleted")
				} else {
					err = e
					loggers.Error.WithField("id", id).Printf("bundle rm: %s", err)
					failedIds = append(failedIds, id)
				}
			} else {
				err = e
				loggers.Error.WithField("id", id).Printf("bundle rm: %s", err)
				failedIds = append(failedIds, id)
			}
		} else {
			err = e
			loggers.Error.WithField("id", id).Printf("bundle rm: %s", err)
			failedIds = append(failedIds, id)
		}
	}
	if err != nil {
		err = fmt.Errorf("cannot delete bundles %+v", failedIds)
	}
	return
}

func runBundleGc(cmd *cobra.Command, args []string) error {
	if len(args) > 0 {
		return usageError("No args expected")
	}
	gcd, err := store.BundleGC(flagBundleTTL)
	for _, b := range gcd {
		os.Stdout.WriteString(b.ID() + "\n")
	}
	return err
}

func runBundleRun(cmd *cobra.Command, args []string) (err error) {
	if len(args) != 1 {
		return usageError("Exactly one argument required")
	}

	b, err := bundleByIdOrDir(args[0])
	if err != nil {
		return
	}
	c, err := containerFromBundle(&b)
	if err != nil {
		return
	}

	defer func() {
		if e := c.Close(); e != nil {
			if err == nil {
				err = e
			} else {
				loggers.Error.Println(e)
			}
		}
	}()

	if err = c.Start(); err != nil {
		return
	}

	// Handle signals
	// TODO: reuse from central location with occurence in run package
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM, syscall.SIGKILL)
	go func() {
		<-sigs
		c.Stop()
	}()

	return c.Wait()
}

func containerFromBundle(b *bundle.Bundle) (c run.Container, err error) {
	containers, err := newContainerManager()
	if err != nil {
		return
	}
	lockedBundle, err := b.Lock()
	if err != nil {
		return
	}
	defer func() {
		if e := lockedBundle.Close(); e != nil && err == nil {
			err = e
		}
	}()
	ioe := run.NewStdContainerIO()
	if flagsBundle.stdin {
		ioe.Stdin = os.Stdin
	}
	return containers.NewContainer(&run.ContainerConfig{
		Id:             "",
		Bundle:         lockedBundle,
		Io:             ioe,
		NoPivotRoot:    flagsBundle.noPivot,
		NoNewKeyring:   flagsBundle.noNewKeyring,
		DestroyOnClose: true,
	})
}

func bundleByIdOrDir(idOrDir string) (b bundle.Bundle, err error) {
	if isFile(idOrDir) {
		b, err = bundle.NewBundle(idOrDir)
	} else {
		b, err = store.Bundle(idOrDir)
	}
	return
}
