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
	netCmd = &cobra.Command{
		Use:   "net",
		Short: "OCI-hook compatible network manager",
		Long: `Subcommands below this command support initialization and destruction 
of container networks and are meant to be declared as hooks of an OCI runtime bundle.`,
	}
	netInitCmd = &cobra.Command{
		Use:   "init",
		Short: "Initializes container networks",
		Long: `Initializes a container's networks.
The OCI container state JSON [1] is expected on stdin.
See OCI state spec at https://github.com/opencontainers/runtime-spec/blob/master/runtime.md`,
		Run: func(cmd *cobra.Command, args []string) {
			panic("TODO: init container networks")
		},
	}
	netRemoveCmd = &cobra.Command{
		Use:   "rm",
		Short: "Removes container networks",
		Long: `Removes a container's networks.
The OCI container state JSON is expected on stdin.
See OCI state spec at https://github.com/opencontainers/runtime-spec/blob/master/runtime.md`,
		Run: func(cmd *cobra.Command, args []string) {
			panic("TODO: destroy container networks")
		},
	}
)

func init() {
	netCmd.AddCommand(netInitCmd)
	netCmd.AddCommand(netRemoveCmd)
}
