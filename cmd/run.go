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
	"github.com/mgoltzsche/cntnr/model"
	"github.com/spf13/cobra"
)

var (
	runCmd = &cobra.Command{
		Use:   "run [flags] IMAGE1 [COMMAND1] [--- [flags] IMAGE2 [COMMAND2]]...",
		Short: "Runs a container",
		Long:  `Runs a container.`,
		Run:   wrapRun(runRun),
	}

/*
	TODO:
	flagHealthCheck     *Check
	flagStopGracePeriod time.Duration*/
)

func init() {
	flagsBundle.InitFlags(runCmd.Flags())
	flagsBundle.InitRunFlags(runCmd.Flags())
}

func runRun(cmd *cobra.Command, args []string) (err error) {
	argSet := split(args, "---")
	services := make([]model.Service, 0, len(argSet))
	if err := flagsBundle.SetBundleArgs(argSet[0]); err != nil {
		return err
	}
	service, err := flagsBundle.Read()
	if err != nil {
		return
	}
	services = append(services, *service)
	for _, a := range argSet[1:] {
		if err = cmd.Flags().Parse(a); err != nil {
			return usageError(err.Error())
		}
		if err = flagsBundle.SetBundleArgs(cmd.Flags().Args()); err != nil {
			return
		}
		service, e := flagsBundle.Read()
		if e != nil {
			return
		}
		services = append(services, *service)
	}

	return runServices(services, resourceResolver("", map[string]model.Volume{}))
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
