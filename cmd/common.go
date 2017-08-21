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
	"github.com/containers/image/types"
	"github.com/mgoltzsche/cntnr/images"
	"github.com/mgoltzsche/cntnr/model"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"os"
	"path/filepath"
)

func handleError(cf func(cmd *cobra.Command, args []string) error) func(cmd *cobra.Command, args []string) {
	return func(cmd *cobra.Command, args []string) {
		err := cf(cmd, args)
		if err != nil {
			msg := err.Error()
			exitCode := 2
			switch err.(type) {
			case UsageError:
				msg = fmt.Sprintf("Error: %s\n%s\n%s\n", msg, cmd.UsageString(), msg)
				exitCode = 1
			}
			os.Stderr.WriteString(msg)
			os.Exit(exitCode)
		}
	}
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

func initImageFlags(f *pflag.FlagSet) {
	defaultImgDir := filepath.Join(currUser.HomeDir, ".cntnr", "images")
	f.StringVar(&flagImgDir, "image-dir", defaultImgDir, "directory to store images")
}

func initContainerFlags(f *pflag.FlagSet) {
	initImageFlags(f)
	f.BoolVar(&flagRootless, "rootless", currUser.Uid != "0", "enables container execution as unprivileged user")
}

func newImages() (*images.Images, error) {
	imgCtx := imageContext()
	// TODO: expose --image-pull-policy CLI option
	imgs, err := images.NewImages(flagImgDir, images.PULL_NEW, imgCtx, debugLog)
	if err != nil {
		return nil, fmt.Errorf("Could not init images: %s", err)
	}
	return imgs, nil
}

func imageContext() *types.SystemContext {
	// TODO: provide CLI options
	c := &types.SystemContext{
		RegistriesDirPath:           "",
		DockerCertPath:              "",
		DockerInsecureSkipTLSVerify: true,
		OSTreeTmpDirPath:            "ostree-tmp-dir",
		// TODO: add docker auth
		//DockerAuthConfig: dockerAuth,
	}

	if flagRootless {
		c.DockerCertPath = "./docker-cert"
	}

	return c
}

func createRuntimeBundle(s *model.Service, imgs *images.Images, vols model.VolumeResolver, id, dir string) (*model.RuntimeBundleBuilder, error) {
	b, err := s.NewRuntimeBundleBuilder(id, dir, imgs, vols, flagRootless)
	if err != nil {
		return nil, err
	}
	if err := b.Build(debugLog); err != nil {
		return nil, err
	}
	return b, nil
}
