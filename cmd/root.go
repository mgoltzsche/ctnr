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
	"github.com/mgoltzsche/cntnr/log"
	"github.com/spf13/cobra"
	//homedir "github.com/mitchellh/go-homedir"
	//"github.com/spf13/viper"
	"github.com/containers/image/types"
	"github.com/mgoltzsche/cntnr/bundles"
	"github.com/mgoltzsche/cntnr/images"
	"github.com/mgoltzsche/cntnr/run"
	"os"
	"os/user"
	"path/filepath"
)

var (
	flagVerbose        bool
	flagRootless       bool
	flagCfgFile        string
	flagBundleStoreDir string
	flagImgStoreDir    string
	flagStateDir       string

	bundleMngr    *bundles.Bundles
	imageMngr     *images.Images
	containerMngr *run.ContainerManager
	errorLog      = log.NewStdLogger(os.Stderr)
	warnLog       = log.NewStdLogger(os.Stderr)
	debugLog      = log.NewNopLogger()
)

// RootCmd represents the base command when called without any subcommands
var RootCmd = &cobra.Command{
	Use:   "cntnr",
	Short: "a lightweight container engine",
	Long: `cntnr is a lightweight OCI-compliant container engine.
It supports single image and container operations as well as high-level service composition.`,
	PersistentPreRun: preRun,
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	RootCmd.AddCommand(runCmd)
	RootCmd.AddCommand(listCmd)
	RootCmd.AddCommand(imageCmd)
	RootCmd.AddCommand(bundleCmd)
	RootCmd.AddCommand(composeCmd)
	RootCmd.AddCommand(netCmd)
	if err := RootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func init() {
	//cobra.OnInitialize(initConfig)

	// Here you will define your flags and configuration settings.
	// Cobra supports persistent flags, which, if defined here,
	// will be global for your application.
	//RootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.cntnr.yaml)")

	currUser, err := user.Current()
	if err != nil {
		exitError(2, "Cannot get current user: %s", err)
	}
	flagImgStoreDir = filepath.Join(currUser.HomeDir, ".cntnr", "images")
	flagBundleStoreDir = filepath.Join(currUser.HomeDir, ".cntnr", "bundles")
	flagStateDir = "/run/cntnr"
	if currUser.Uid != "0" {
		flagStateDir = "/run/user/" + currUser.Uid + "/cntnr"
	}
	f := RootCmd.PersistentFlags()
	f.BoolVar(&flagVerbose, "verbose", false, "enables verbose log output")
	f.BoolVar(&flagRootless, "rootless", currUser.Uid != "0", "enables image and container management as unprivileged user")
	f.StringVar(&flagImgStoreDir, "image-store-dir", flagImgStoreDir, "directory to store images")
	f.StringVar(&flagBundleStoreDir, "bundle-store-dir", flagBundleStoreDir, "directory to store OCI runtime bundles")
	f.StringVar(&flagStateDir, "state-dir", flagStateDir, "directory to store OCI container states (should be tmpfs)")
}

func preRun(cmd *cobra.Command, args []string) {
	var err error

	if flagVerbose {
		debugLog = log.NewStdLogger(os.Stderr)
	}

	// Init image store
	// TODO: provide CLI options
	ctx := &types.SystemContext{
		RegistriesDirPath:           "",
		DockerCertPath:              "",
		DockerInsecureSkipTLSVerify: true,
		OSTreeTmpDirPath:            "ostree-tmp-dir",
		// TODO: add docker auth
		//DockerAuthConfig: dockerAuth,
	}
	if flagRootless {
		ctx.DockerCertPath = "./docker-cert"
	}
	// TODO: expose --image-pull-policy CLI option
	imageMngr, err = images.NewImages(flagImgStoreDir, images.PULL_NEW, ctx, debugLog)
	exitOnError(cmd, err)

	// init bundle store
	bundleMngr, err = bundles.NewBundles(flagBundleStoreDir)
	exitOnError(cmd, err)

	// init container manager
	containerMngr, err = run.NewContainerManager(flagStateDir, debugLog)
	exitOnError(cmd, err)
}

// initConfig reads in config file and ENV variables if set.
/*func initConfig() {
	if flagCfgFile != "" {
		// Use config file from the flag.
		viper.SetConfigFile(flagCfgFile)
	} else {
		// Find home directory.
		home, err := homedir.Dir()
		if err != nil {
			exitError(1, "%s", err)
		}

		// Search config in home directory with name ".cntnr" (without extension).
		viper.AddConfigPath(home)
		viper.SetConfigName(".cntnr")
	}

	viper.AutomaticEnv() // read in environment variables that match

	// If a config file is found, read it in.
	if err := viper.ReadInConfig(); err == nil {
		fmt.Println("Using config file:", viper.ConfigFileUsed())
	}
}*/
