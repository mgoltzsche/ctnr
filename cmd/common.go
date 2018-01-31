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

	"github.com/hashicorp/go-multierror"
	"github.com/mgoltzsche/cntnr/model"
	"github.com/mgoltzsche/cntnr/oci/bundle"
	"github.com/mgoltzsche/cntnr/oci/image"
	"github.com/mgoltzsche/cntnr/run"
	"github.com/mgoltzsche/cntnr/run/factory"
	"github.com/spf13/cobra"
)

func wrapRun(cf func(cmd *cobra.Command, args []string) error) func(cmd *cobra.Command, args []string) {
	return func(cmd *cobra.Command, args []string) {
		err := cf(cmd, args)
		closeLockedImageStore()
		exitOnError(cmd, err)
	}
}

func exitOnError(cmd *cobra.Command, err error) {
	if err == nil {
		return
	}
	switch err.(type) {
	case UsageError:
		logger.Errorf("%s\n%s\n%s", err, cmd.UsageString(), err)
		os.Exit(1)
	case *run.ExitError:
		exiterr := err.(*run.ExitError)
		logger.WithField("id", exiterr.ContainerID()).WithField("status", exiterr.Status()).Error("Container terminated")
		os.Exit(exiterr.Status())
	default:
		logger.Error(err)
	}
	os.Exit(255)
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

func openImageStore() (image.ImageStoreRW, error) {
	if lockedImageStore == nil {
		s, err := store.OpenLockedImageStore()
		if err != nil {
			return nil, err
		}
		lockedImageStore = s
	}
	return lockedImageStore, nil
}

func closeLockedImageStore() {
	if lockedImageStore != nil {
		lockedImageStore.Close()
	}
}

func newContainerManager() (run.ContainerManager, error) {
	return factory.NewContainerManager(flagStateDir, flagRootless, loggers)
}

func resourceResolver(baseDir string, volumes map[string]model.Volume) model.ResourceResolver {
	paths := model.NewPathResolver(baseDir)
	return model.NewResourceResolver(paths, volumes)
}

func runServices(services []model.Service, res model.ResourceResolver) (err error) {
	manager, err := newContainerManager()
	if err != nil {
		return
	}

	containers := run.NewContainerGroup(loggers.Debug)
	defer func() {
		e := containers.Close()
		err = run.WrapExitError(err, e)
	}()

	for _, s := range services {
		var c run.Container
		loggers.Debug.Println(s.JSON())
		if c, err = createContainer(&s, res, manager); err != nil {
			return
		}
		containers.Add(c)
	}

	closeLockedImageStore()
	containers.Start()
	containers.Wait()
	return
}

func createContainer(model *model.Service, res model.ResourceResolver, manager run.ContainerManager) (c run.Container, err error) {
	var bundle *bundle.LockedBundle
	if bundle, err = createRuntimeBundle(model, res); err != nil {
		return
	}
	defer func() {
		if err != nil {
			if e := bundle.Close(); e != nil {
				err = multierror.Append(err, e)
			}
		}
	}()

	ioe := run.NewStdContainerIO()
	if model.StdinOpen {
		ioe.Stdin = os.Stdin
	}

	return manager.NewContainer(&run.ContainerConfig{
		Id:           "",
		Bundle:       bundle,
		Io:           ioe,
		NoNewKeyring: model.NoNewKeyring,
		NoPivotRoot:  model.NoPivot,
	})
}

func createRuntimeBundle(service *model.Service, res model.ResourceResolver) (b *bundle.LockedBundle, err error) {
	if service.Image == "" {
		return nil, fmt.Errorf("service %q has no image", service.Name)
	}

	istore, err := openImageStore()
	if err != nil {
		return
	}

	bundleId := service.Bundle
	bundleDir := ""
	if isFile(bundleId) {
		bundleDir = bundleId
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
	if err = service.ToSpec(res, flagRootless, flagPRootPath, builder.SpecBuilder); err != nil {
		return
	}

	// Create bundle
	if bundleDir != "" {
		b, err = builder.Build(bundleDir, service.BundleUpdate)
	} else {
		b, err = store.CreateBundle(builder, service.BundleUpdate)
	}
	return
}

func isFile(file string) bool {
	return file != "" && (filepath.IsAbs(file) || file == "." || file == ".." || len(file) > 1 && file[0:2] == "./" || len(file) > 2 && file[0:3] == "../" || file == "~" || len(file) > 1 && file[0:2] == "~/")
}
