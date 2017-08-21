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
	"os"
	"os/user"
)

var (
	flagVerbose       bool
	flagImgDir        string
	flagBundleBaseDir string
	flagRootless      bool
	flagCfgFile       string

	currUser *user.User

	errorLog = log.NewStdLogger(os.Stderr)
	warnLog  = log.NewStdLogger(os.Stderr)
	debugLog = log.NewNopLogger()
)

// RootCmd represents the base command when called without any subcommands
var RootCmd = &cobra.Command{
	Use:   "cntnr",
	Short: "a lightweight container engine",
	Long: `cntnr is a lightweight OCI-compliant container engine.
It supports single image and container operations as well as high-level service composition.`,
	PersistentPreRun: func(cmd *cobra.Command, a []string) {
		if flagVerbose {
			debugLog = log.NewStdLogger(os.Stderr)
		}
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	RootCmd.AddCommand(listCmd)
	RootCmd.AddCommand(runCmd)
	RootCmd.AddCommand(imageCmd)
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

	RootCmd.Flags().BoolVar(&flagVerbose, "verbose", false, "enables verbose log output")

	usr, err := user.Current()
	if err != nil {
		exitError(2, "Cannot get current user: %s", err)
	}
	currUser = usr
	if flagVerbose {
		debugLog = log.NewStdLogger(os.Stderr)
	}
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
