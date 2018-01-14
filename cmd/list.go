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

	"github.com/mgoltzsche/cntnr/run/factory"
	"github.com/spf13/cobra"
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "Lists all active containers in the local store (--state-dir)",
	Long:  `Lists all containers in the local store.`,
	Run:   wrapRun(runList),
}

func runList(cmd *cobra.Command, args []string) error {
	if len(args) > 0 {
		return usageError("No args expected")
	}

	containers, err := factory.NewContainerManager(flagStateDir, flagRootless, debugLog)
	if err != nil {
		return err
	}

	l, err := containers.List()
	if err != nil {
		return err
	}
	// TODO: print pid, created, image (annotation) and ip
	f := "%-26s  %-10s\n"
	fmt.Printf(f, "ID", "STATUS")
	for _, c := range l {
		fmt.Printf(f, c.ID, c.Status)
	}
	return nil
}
