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
	"github.com/satori/go.uuid"
	"github.com/spf13/cobra"
)

var (
	runCmd = &cobra.Command{
		Use:   "run [flags] IMAGE1 [COMMAND1] [--- [flags] IMAGE2 [COMMAND2]]...",
		Short: "Runs a container",
		Long:  `Runs a container.`,
		Run:   handleError(runRun),
	}

/*
	TODO:
	flagHealthCheck     *Check
	flagStopGracePeriod time.Duration*/
)

func init() {
	initBundleRunFlags(runCmd.Flags())
}

func runRun(cmd *cobra.Command, args []string) error {
	argSet := split(args, "---")
	if err := flagsBundle.setBundleArgs(argSet[0]); err != nil {
		return err
	}
	for _, a := range argSet[1:] {
		flagsBundle.add()
		if err := cmd.Flags().Parse(a); err != nil {
			return usageError(err.Error())
		}
		if err := flagsBundle.setBundleArgs(cmd.Flags().Args()); err != nil {
			return err
		}
	}
	project := &model.Project{}
	for _, s := range flagsBundle.apps {
		fmt.Println(s.JSON())
		bundle, err := createRuntimeBundle(project, s, "")
		if err != nil {
			return err
		}
		containerId := uuid.NewV4().String()
		c, err := containerMngr.NewContainer(containerId, bundle.Dir, bundle.Spec, s.StdinOpen)
		if err != nil {
			return err
		}

		if err = containerMngr.Deploy(c); err != nil {
			containerMngr.Stop()
			return err
		}
	}
	containerMngr.HandleSignals()
	return containerMngr.Wait()
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
