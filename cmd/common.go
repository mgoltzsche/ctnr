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
	"path/filepath"

	"github.com/mgoltzsche/cntnr/model"
	"github.com/mgoltzsche/cntnr/run"
	"github.com/spf13/cobra"
)

func handleError(cf func(cmd *cobra.Command, args []string) error) func(cmd *cobra.Command, args []string) {
	return func(cmd *cobra.Command, args []string) {
		exitOnError(cmd, cf(cmd, args))
	}
}

func exitOnError(cmd *cobra.Command, err error) {
	if err == nil {
		return
	}
	msg := err.Error()
	exitCode := 2
	switch err.(type) {
	case UsageError:
		msg = fmt.Sprintf("Error: %s\n%s\n%s\n", msg, cmd.UsageString(), msg)
		exitCode = 1
	default:
		msg = msg + "\n"
	}
	os.Stderr.WriteString(msg)
	os.Exit(exitCode)
}

func usageError(msg string) UsageError {
	/*var buf bytes.Buffer
	cmd.SetOutput(&buf)
	cmd.HelpFunc()(cmd, args)
	cmd.SetOutput(nil)
	return fmt.Errorf("Error: %s\n%s\n%s", msg, buf.String(), msg)*/
	return UsageError(msg)
}

type UsageError string

func (err UsageError) Error() string {
	return string(err)
}

func exitError(exitCode int, frmt string, values ...interface{}) {
	os.Stderr.WriteString(fmt.Sprintf(frmt+"\n", values...))
	os.Exit(exitCode)
}

func runProject(project *model.Project) error {
	for _, s := range project.Services {
		fmt.Println(s.JSON())
		bundle, err := createRuntimeBundle(project, &s, "")
		if err != nil {
			return err
		}
		c, err := containerMngr.NewContainer("", bundle.Dir, bundle.Spec.Spec(), s.StdinOpen)
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

func createRuntimeBundle(p *model.Project, s *model.Service, bundleDir string) (*model.RuntimeBundleBuilder, error) {
	bundleId := run.GenerateId()
	if bundleDir == "" {
		bundleDir = filepath.Join(bundleMngr.Dir(), bundleId)
	} else {
		absDir, err := filepath.Abs(bundleDir)
		if err != nil {
			return nil, err
		}
		bundleDir = absDir
	}
	vols := model.NewVolumeResolver(p, bundleDir)
	b, err := s.NewRuntimeBundleBuilder(bundleId, bundleDir, imageMngr, vols, flagRootless)
	if err != nil {
		return nil, err
	}
	if err := b.Build(debugLog); err != nil {
		return nil, err
	}
	return b, nil
}
