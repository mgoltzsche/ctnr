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
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"

	"github.com/mgoltzsche/cntnr/bundle"
	"github.com/mgoltzsche/cntnr/image"
	"github.com/mgoltzsche/cntnr/model"
	"github.com/mgoltzsche/cntnr/model/oci"
	exterrors "github.com/mgoltzsche/cntnr/pkg/errors"
	"github.com/mgoltzsche/cntnr/run"
	"github.com/mgoltzsche/cntnr/run/factory"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

func wrapRun(cf func(cmd *cobra.Command, args []string) error) func(cmd *cobra.Command, args []string) {
	return func(cmd *cobra.Command, args []string) {
		defer func() {
			if err := recover(); err != nil {
				msg := "\n  OUPS, THIS SEEMS TO BE A BUG! xl"
				msg += "\n  Please report it at"
				msg += "\n    https://github.com/mgoltzsche/cntnr/issues/new"
				msg += "\n  with a description of what you did and the stacktrace"
				msg += "\n  below if you cannot find an already existing issue at"
				msg += "\n    https://github.com/mgoltzsche/cntnr/issues\n"
				stackTrace := strings.Replace(string(debug.Stack()), "\n", "\n  ", -1)
				// TODO: Add version
				logrus.Fatalf("%+v\n%s\n  PANIC: %s\n  %s", err, msg, err, stackTrace)
				os.Exit(255)
			}
		}()
		err := cf(cmd, args)
		closeLockedImageStore()
		exitOnError(cmd, err)
	}
}

func exitOnError(cmd *cobra.Command, err error) {
	if err == nil {
		return
	}

	// Usage error - print help text and exit
	if _, ok := err.(UsageError); ok {
		logger.Errorf("%s\n%s\n%s", err, cmd.UsageString(), err)
		os.Exit(1)
	}

	// Handle exit error
	exitCode := 255
	errLog := loggers.Error
	cause := errors.Cause(err)
	if exitErr, ok := cause.(*run.ExitError); ok {
		exitCode = exitErr.Code()
		errLog = errLog.WithField("id", exitErr.ContainerID()).WithField("code", exitCode)
		err = errors.New("container process terminated with error")
	}

	// Print error & exit
	errStr := err.Error()
	causeStr := fmt.Sprintf("%+v", cause)
	if causeStr != errStr {
		loggers.Debug.Println(strings.Replace(causeStr, "\n", "\n  ", -1))
	}
	errLog.Println(errStr)
	os.Exit(exitCode)
}

func usageError(msg string) UsageError {
	return UsageError(msg)
}

type UsageError string

func (err UsageError) Error() string {
	return string(err)
}

func exitError(exitCode int, frmt string, values ...interface{}) {
	loggers.Error.Printf(frmt, values...)
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
		err = exterrors.Append(err, containers.Close())
	}()

	for _, s := range services {
		var c run.Container
		loggers.Debug.Println(s.JSON())
		if c, err = createContainer(&s, res, manager, true); err != nil {
			return
		}
		containers.Add(c)
	}

	closeLockedImageStore()
	containers.Start()
	containers.Wait()
	return
}

func createContainer(model *model.Service, res model.ResourceResolver, manager run.ContainerManager, destroyOnClose bool) (c run.Container, err error) {
	var bundle *bundle.LockedBundle
	if bundle, err = createRuntimeBundle(model, res); err != nil {
		return
	}
	defer func() {
		if err != nil {
			err = exterrors.Append(err, bundle.Close())
		}
	}()

	ioe := run.NewStdContainerIO()
	if model.StdinOpen {
		ioe.Stdin = os.Stdin
	}

	return manager.NewContainer(&run.ContainerConfig{
		Id:             "",
		Bundle:         bundle,
		Io:             ioe,
		NoNewKeyring:   model.NoNewKeyring,
		NoPivotRoot:    model.NoPivot,
		DestroyOnClose: destroyOnClose,
	})
}

func createRuntimeBundle(service *model.Service, res model.ResourceResolver) (b *bundle.LockedBundle, err error) {
	if service.Image == "" {
		return nil, errors.Errorf("service %q has no image", service.Name)
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
	if err = oci.ToSpec(service, res, flagRootless, flagPRootPath, builder.SpecBuilder); err != nil {
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

func checkNonEmpty(s string) (err error) {
	if len(bytes.TrimSpace([]byte(s))) == 0 {
		err = usageError("empty value")
	}
	return
}
