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
	"github.com/mgoltzsche/cntnr/model"
	"github.com/mgoltzsche/cntnr/run"
	"github.com/satori/go.uuid"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"path/filepath"
	"strconv"
)

var (
	runCmd = &cobra.Command{
		Use:   "run [flags] IMAGE1 [ARG11[, ARG12...]] [--- [flags] IMAGE2 [ARG21[, ARG22...]]]...",
		Short: "Runs a container",
		Long:  `Runs a container.`,
		Run:   handleError(runRun),
	}

	cntnrs = &apps{[]*model.Service{}}

/*	flagDns             []string
	flagDnsSearch       []string
	flagExtraHosts      []ExtraHost
	flagEntrypoint      []string
	flagEnvironment     map[string]string
	flagExpose          []string
	flagPorts           []PortBinding
	flagVolumes         []VolumeMount
	flagHealthCheck     *Check
	flagStopGracePeriod time.Duration*/
)

func init() {
	cntnrs.add()
	initContainerFlags(runCmd.Flags())
	initBundleFlags(runCmd.Flags(), cntnrs)
}

func initBundleFlags(f *pflag.FlagSet, c *apps) {
	defaultBundleStoreDir := filepath.Join(currUser.HomeDir, ".cntnr", "containers")
	f.StringVar(&flagBundleStoreDir, "bundle-store-dir", defaultBundleStoreDir, "directory to store OCI runtime bundles")
	f.Var((*cName)(c), "name", "container name and implicit hostname when hostname is not set explicitly")
	f.Var((*cHostname)(c), "hostname", "container hostname")
	f.Var((*cDomainname)(c), "domainname", "container domainname")
	f.Var((*cStdin)(c), "stdin", "binds stdin to the container")
	f.Var((*cTty)(c), "tty", "binds the terminal to the container")
	f.Var((*cReadOnly)(c), "readonly", "mounts the root file system in read only mode")
	// Stop parsing after first non flag argument (image)
	f.SetInterspersed(false)
}

func split(args []string, sep string) [][]string {
	r := [][]string{}
	c := []string{}
	for _, arg := range args {
		if arg == sep {
			r = append(r, c)
			c = []string{}
		} else {
			c = append(c, arg)
		}
	}
	r = append(r, c)
	return r
}

func runRun(cmd *cobra.Command, args []string) error {
	argSet := split(args, "---")
	err := applyContainerArgs(cmd, argSet[0])
	if err != nil {
		return err
	}
	for _, a := range argSet[1:] {
		cntnrs.add()
		err = cmd.Flags().Parse(a)
		if err != nil {
			return usageError(err.Error())
		}
		err = applyContainerArgs(cmd, cmd.Flags().Args())
		if err != nil {
			return err
		}
	}
	imgs, err := newImages()
	if err != nil {
		return err
	}
	// TODO: provide cli option
	rootDir := "/run/runc"
	if flagRootless {
		rootDir = "/tmp/runc"
	}
	manager := run.NewContainerManager(debugLog)
	project := &model.Project{}
	for _, s := range cntnrs.apps {
		fmt.Println(s.JSON())
		containerId := uuid.NewV4().String()
		if s.Name == "" {
			s.Name = containerId
		}
		bundleDir := filepath.Join(flagBundleStoreDir, containerId)
		vols := model.NewVolumeResolver(project, bundleDir)
		b, err := createRuntimeBundle(s, imgs, vols, containerId, bundleDir)
		if err != nil {
			return err
		}

		c, err := run.NewContainer(containerId, b.Dir, rootDir, b.Spec, s.StdinOpen, errorLog, debugLog)
		if err != nil {
			return err
		}

		err = manager.Deploy(c)
		if err != nil {
			manager.Stop()
			return err
		}
	}
	manager.HandleSignals()
	return manager.Wait()
}

func applyContainerArgs(cmd *cobra.Command, ca []string) error {
	if len(ca) == 0 {
		return usageError("No image arg specified")
	}
	last := cntnrs.last()
	last.Image = ca[0]
	if len(ca) > 1 {
		last.Command = ca[1:]
	}
	return nil
}

type apps struct {
	apps []*model.Service
}

func (c *apps) add() {
	c.apps = append(c.apps, model.NewService(""))
}

func (c *apps) last() *model.Service {
	return c.apps[len(c.apps)-1]
}

type cName apps

func (c *cName) Set(s string) error {
	(*apps)(c).last().Name = s
	return nil
}

func (c *cName) Type() string {
	return "string"
}

func (c *cName) String() string {
	return (*apps)(c).last().Name
}

type cHostname apps

func (c *cHostname) Set(s string) error {
	(*apps)(c).last().Hostname = s
	return nil
}

func (c *cHostname) Type() string {
	return "string"
}

func (c *cHostname) String() string {
	return (*apps)(c).last().Hostname
}

type cDomainname apps

func (c *cDomainname) Set(s string) error {
	(*apps)(c).last().Domainname = s
	return nil
}

func (c *cDomainname) Type() string {
	return "string"
}

func (c *cDomainname) String() string {
	return (*apps)(c).last().Domainname
}

type cStdin apps

func (c *cStdin) Set(s string) (err error) {
	(*apps)(c).last().StdinOpen, err = parseBool(s)
	return
}

func (c *cStdin) Type() string {
	return "bool"
}

func (c *cStdin) String() string {
	return strconv.FormatBool((*apps)(c).last().StdinOpen)
}

type cTty apps

func (c *cTty) Set(s string) (err error) {
	(*apps)(c).last().Tty, err = parseBool(s)
	return
}

func (c *cTty) Type() string {
	return "bool"
}

func (c *cTty) String() string {
	return strconv.FormatBool((*apps)(c).last().Tty)
}

type cReadOnly apps

func (c *cReadOnly) Set(s string) (err error) {
	(*apps)(c).last().ReadOnly, err = parseBool(s)
	return
}

func (c *cReadOnly) Type() string {
	return "bool"
}

func (c *cReadOnly) String() string {
	return strconv.FormatBool((*apps)(c).last().ReadOnly)
}

func parseBool(s string) (bool, error) {
	b, err := strconv.ParseBool(s)
	if err != nil {
		err = fmt.Errorf("Only 'true' or 'false' are accepted values")
	}
	return b, err
}
