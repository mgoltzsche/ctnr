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
	exterrors "github.com/mgoltzsche/cntnr/pkg/errors"
	"github.com/spf13/cobra"
)

var (
	killCmd = &cobra.Command{
		Use:   "kill [flags] CONTAINERID",
		Short: "Kills a running container",
		Long:  `Kills a running container.`,
		Run:   wrapRun(runKill),
	}
	flagSignal string
	flagAll    bool
)

func init() {
	killCmd.Flags().StringVarP(&flagSignal, "signal", "s", "TERM", "Signal to be sent to container process")
	killCmd.Flags().BoolVarP(&flagAll, "all", "a", false, "Send the specified signal to all processes inside the container")
}

func runKill(cmd *cobra.Command, args []string) (err error) {
	if len(args) == 0 {
		return usageError("At least one container ID argument expected")
	}

	containers, err := newContainerManager()
	if err != nil {
		return err
	}

	for _, id := range args {
		if e := containers.Kill(id, flagSignal, flagAll); e != nil {
			loggers.Debug.Println("Failed to kill container:", e)
			err = exterrors.Append(err, e)
		}
	}
	return
}
