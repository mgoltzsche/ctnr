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
	"github.com/mgoltzsche/cntnr/model"
	"github.com/mgoltzsche/cntnr/run"
	"github.com/satori/go.uuid"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"path/filepath"
)

var (
	runCmd = &cobra.Command{
		Use:   "run [flags] IMAGE1 [I1CMD1[, I1CMD2...]] [--- [flags] IMAGE2 [I2CMD1[, I2CMD2...]]]...",
		Short: "Runs a container",
		Long:  `Runs a container.`,
		Run:   handleError(runRun),
	}

	cntnrs = &apps{netCfg{nil}, []*model.Service{}}

/*
	TODO:
	flagHealthCheck     *Check
	flagStopGracePeriod time.Duration*/
)

func init() {
	cntnrs.add()
	initContainerFlags(runCmd.Flags())
	initBundleFlags(runCmd.Flags(), cntnrs)
}

func initBundleFlags(f *pflag.FlagSet, c *apps) {
	defaultBundleStoreDir := filepath.Join(currUser.HomeDir, ".cntnr", "containers")
	f.StringVar(&flagBundleStoreDir, "bundle-store-dir", defaultBundleStoreDir, "directory to store OCI runtime bundles")
	f.Var((*cName)(c), "name", "container name and implicit hostname when hostname is not set explicitly")
	f.Var((*cEntrypoint)(c), "entrypoint", "container entrypoint")
	f.Var((*cEnvironment)(c), "env", "container environment variables")
	f.Var((*cVolumeMount)(c), "mount", "container volume mounts: TARGET|SOURCE:TARGET[:OPTIONS]")
	f.Var((*cExpose)(c), "expose", "container ports to be exposed")
	f.Var((*cStdin)(c), "stdin", "binds stdin to the container")
	f.Var((*cTty)(c), "tty", "binds the terminal to the container")
	f.Var((*cReadOnly)(c), "readonly", "mounts the root file system in read only mode")
	initNetConfFlags(f, &c.netCfg)
	// Stop parsing after first non flag argument (image)
	f.SetInterspersed(false)
}

func runRun(cmd *cobra.Command, args []string) error {
	argSet := split(args, "---")
	err := applyContainerArgs(cmd, argSet[0])
	if err != nil {
		return err
	}
	for _, a := range argSet[1:] {
		cntnrs.add()
		err = cmd.Flags().Parse(a)
		if err != nil {
			return usageError(err.Error())
		}
		err = applyContainerArgs(cmd, cmd.Flags().Args())
		if err != nil {
			return err
		}
	}
	imgs, err := newImages()
	if err != nil {
		return err
	}
	// TODO: provide cli option
	rootDir := "/run/runc"
	if flagRootless {
		rootDir = "/tmp/runc"
	}
	manager := run.NewContainerManager(debugLog)
	project := &model.Project{}
	for _, s := range cntnrs.apps {
		fmt.Println(s.JSON())
		containerId := uuid.NewV4().String()
		if s.Name == "" {
			s.Name = containerId
		}
		bundleDir := filepath.Join(flagBundleStoreDir, containerId)
		vols := model.NewVolumeResolver(project, bundleDir)
		b, err := createRuntimeBundle(s, imgs, vols, containerId, bundleDir)
		if err != nil {
			return err
		}

		c, err := run.NewContainer(containerId, b.Dir, rootDir, b.Spec, s.StdinOpen, errorLog, debugLog)
		if err != nil {
			return err
		}

		err = manager.Deploy(c)
		if err != nil {
			manager.Stop()
			return err
		}
	}
	manager.HandleSignals()
	return manager.Wait()
}

func applyContainerArgs(cmd *cobra.Command, ca []string) error {
	if len(ca) == 0 {
		return usageError("No image arg specified")
	}
	last := cntnrs.last()
	last.Image = ca[0]
	if len(ca) > 1 {
		last.Command = ca[1:]
	}
	return nil
}

func split(args []string, sep string) [][]string {
	r := [][]string{}
	c := []string{}
	for _, arg := range args {
		if arg == sep {
			r = append(r, c)
			c = []string{}
		} else {
			c = append(c, arg)
		}
	}
	r = append(r, c)
	return r
}
