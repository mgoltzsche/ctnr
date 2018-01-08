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
	"github.com/mgoltzsche/cntnr/oci/bundle"
	"github.com/mgoltzsche/cntnr/oci/image"
	"github.com/mgoltzsche/cntnr/run"
	"github.com/mgoltzsche/cntnr/run/factory"
	"github.com/spf13/cobra"
)

func handleError(cf func(cmd *cobra.Command, args []string) error) func(cmd *cobra.Command, args []string) {
	return func(cmd *cobra.Command, args []string) {
		err := cf(cmd, args)
		if exitErr, ok := err.(*run.ExitError); ok {
			os.Exit(exitErr.Status())
		} else {
			exitOnError(cmd, err)
		}
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

func runProject(project *model.Project) (err error) {
	manager, err := factory.NewContainerManager(flagStateDir, flagRootless, debugLog)
	if err != nil {
		return
	}

	istore, err := store.OpenLockedImageStore()
	if err != nil {
		return
	}
	defer istore.Close()

	containers := run.NewContainerGroup(debugLog)
	containers.HandleSignals()
	defer func() {
		err = run.WrapExitError(err, containers.Close())
	}()

	for _, s := range project.Services {
		fmt.Println(s.JSON())
		var bundle *bundle.LockedBundle
		if bundle, err = createRuntimeBundle(istore, project, &s, "", false); err != nil {
			return err
		}

		ioe := run.NewStdContainerIO()
		if s.StdinOpen {
			ioe.Stdin = os.Stdin
		}

		var container run.Container
		if container, err = manager.NewContainer("", bundle, ioe); err != nil {
			return err
		}

		if err = containers.Deploy(container); err != nil {
			return err
		}
	}

	istore.Close()
	return containers.Wait()
}

func createRuntimeBundle(istore image.ImageStoreRW, p *model.Project, service *model.Service, bundleIdOrDir string, update bool) (b *bundle.LockedBundle, err error) {
	if service.Image == "" {
		err = fmt.Errorf("service %q has no image", service.Name)
		return
	}

	bundleId := bundleIdOrDir
	bundleDir := ""
	if isFile(bundleIdOrDir) {
		bundleDir = bundleIdOrDir
		bundleId = ""
	}

	// Load image and bundle builder
	var builder *bundle.BundleBuilder
	if service.Image == "" {
		builder = bundle.Builder(bundleId)
	} else {
		var img image.Image
		if img, err = image.GetImage(istore, service.Image); err != nil {
			return
		}
		if builder, err = bundle.BuilderFromImage(bundleId, &img); err != nil {
			return
		}
	}

	// Generate config.json
	if err = service.ToSpec(p, flagRootless, builder.SpecBuilder); err != nil {
		return
	}

	// Create bundle
	if bundleDir != "" {
		b, err = builder.Build(bundleDir, update)
	} else {
		b, err = store.CreateBundle(builder, update)
	}
	return
}

func isFile(file string) bool {
	return file != "" && (filepath.IsAbs(file) || file == "." || file == ".." || len(file) > 1 && file[0:2] == "./" || len(file) > 2 && file[0:3] == "../" || file == "~" || len(file) > 1 && file[0:2] == "~/")
}
