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
	"github.com/mgoltzsche/cntnr/model/compose"
	"github.com/spf13/cobra"
)

var (
	composeCmd = &cobra.Command{
		Use:   "compose",
		Short: "Manage docker compose files",
		Long:  `Converts and runs docker compose files.`,
	}
	composeRunCmd = &cobra.Command{
		Use:   "run [flags] FILE",
		Short: "Run a docker compose file",
		Long:  `Converts and runs a docker compose file.`,
		Run:   wrapRun(runComposeRun),
	}
)

func init() {
	composeCmd.AddCommand(composeRunCmd)
}

func runComposeRun(cmd *cobra.Command, args []string) error {
	if len(args) != 1 {
		return usageError("No compose file argument provided")
	}

	project, err := compose.Load(args[0], "", compose.GetEnv(), loggers.Warn)
	if err != nil {
		return err
	}

	services := make([]model.Service, 0, len(project.Services))
	for _, s := range project.Services {
		services = append(services, s)
	}
	return runServices(services, resourceResolver(project.Dir, project.Volumes))
}
