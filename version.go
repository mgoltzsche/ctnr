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

package main

import (
	"fmt"

	"github.com/mgoltzsche/ctnr/cmd"
	"github.com/spf13/cobra"
)

var (
	version    = "dev"
	commit     = "none"
	date       = "unknown"
	versionCmd = &cobra.Command{
		Use:   "version",
		Short: "print version",
		Long:  `print version.`,
		Run: func(_ *cobra.Command, args []string) {
			fmt.Printf("version: %s\ncommit: %s\ndate: %s\n", version, commit, date)
		},
	}
)

func init() {
	cmd.RootCmd.AddCommand(versionCmd)
}
