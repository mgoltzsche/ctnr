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
	"github.com/mgoltzsche/ctnr/bundle/builder"
	"github.com/mgoltzsche/ctnr/model/oci"
	"github.com/mgoltzsche/ctnr/run"
	"github.com/spf13/cobra"
)

var (
	execCmd = &cobra.Command{
		Use:   "exec [flags] CONTAINERID COMMAND",
		Short: "Executes a process in a container",
		Long:  `Executes a process in a container.`,
		Run:   wrapRun(runExec),
	}

/*
	TODO:
	flagHealthCheck     *Check
	flagStopGracePeriod time.Duration*/
)

func init() {
	flagsBundle.InitProcessFlags(execCmd.Flags())
	flagsBundle.InitRunFlags(execCmd.Flags())
}

func runExec(cmd *cobra.Command, args []string) (err error) {
	if len(args) < 1 {
		return usageError("No CONTAINERID argument specified")
	}
	if len(args) < 2 {
		return usageError("No COMMAND argument specified")
	}
	if err := flagsBundle.SetBundleArgs(args); err != nil {
		return err
	}
	service, err := flagsBundle.Read()
	if err != nil {
		return
	}
	spec := builder.NewSpecBuilder()
	if err = oci.ToSpecProcess(&service.Process, flagPRootPath, &spec); err != nil {
		return
	}
	manager, err := newContainerManager()
	if err != nil {
		return
	}
	container, err := manager.Get(args[0])
	if err != nil {
		return
	}
	sp, err := spec.Spec(container.Rootfs())
	if err != nil {
		return
	}
	proc, err := container.Exec(sp.Process, run.NewStdContainerIO())
	if err != nil {
		return
	}
	defer func() {
		if e := proc.Close(); e != nil && err == nil {
			err = e
		}
	}()
	return proc.Wait()
}
