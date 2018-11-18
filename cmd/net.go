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
	"encoding/json"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"

	"github.com/containernetworking/cni/libcni"
	"github.com/mgoltzsche/ctnr/net"
	"github.com/mitchellh/go-homedir"
	specs "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

var (
	netCmd = &cobra.Command{
		Use:   "net",
		Short: "OCI runtime hooks to setup networking (not to be used outside an OCI hook)",
		Long: `Subcommands below this command support initialization and destruction 
of container networks and are meant to be declared as hooks of an OCI runtime bundle
and not executed manually.`,
	}
	netInitCmd = &cobra.Command{
		Use:   "init",
		Short: "Initializes container networks",
		Long: `Initializes a container's networks.
The OCI container state JSON [1] is expected on stdin.
See OCI state spec at https://github.com/opencontainers/runtime-spec/blob/master/runtime.md`,
		Run: wrapRun(runNetInit),
	}
	netRemoveCmd = &cobra.Command{
		Use:   "rm",
		Short: "Removes container networks",
		Long: `Removes a container's networks.
The OCI container state JSON is expected on stdin.
See OCI state spec at https://github.com/opencontainers/runtime-spec/blob/master/runtime.md`,
		Run: wrapRun(runNetRemove),
	}
)

func init() {
	netCmd.AddCommand(netInitCmd)
	netCmd.AddCommand(netRemoveCmd)

	initNetFlags(netInitCmd.Flags())
	initPortBindFlags(netRemoveCmd.Flags())
}

func runNetInit(cmd *cobra.Command, args []string) (err error) {
	state, err := readContainerState()
	if err != nil {
		return
	}
	spec, err := loadBundleSpec(state)
	if err != nil {
		return
	}

	// Setup networks
	mngr, err := net.NewNetManager(state)
	if err != nil {
		return
	}
	netConfigs, err := loadNetConfigs(args)
	if err != nil {
		return
	}
	defer func() {
		if err != nil {
			// Free all network resources on error
			for i, netConf := range netConfigs {
				mngr.DelNet("cni"+strconv.Itoa(i), netConf)
			}
		}
	}()
	cfg := net.NewConfigFileGenerator()
	for i, netConf := range netConfigs {
		r, err := mngr.AddNet("cni"+strconv.Itoa(i), netConf)
		if err != nil {
			return err
		}
		cfg.AddCniResult(r)
	}

	// Generate hostname, hosts, resolv.conf files
	cfg.SetHostname(spec.Hostname)
	applyArgs(&cfg)
	rootfs := filepath.Join(state.Bundle, spec.Root.Path)
	mounts := filepath.Join(state.Bundle, "mount")
	return cfg.WriteConfigFiles(rootfs, mounts)
}

func runNetRemove(cmd *cobra.Command, args []string) (err error) {
	/*defer func() {
		out := "fine"
		if err != nil {
			out = err.Error()
		} else if e := recover(); e != nil {
			out = fmt.Sprintf("%v", e)
		}
		ioutil.WriteFile("/tmp/postrun-error", []byte(out), 0644)
	}()*/

	state, err := readContainerState()
	if err != nil {
		return
	}
	mngr, err := net.NewNetManager(state)
	if err != nil {
		return
	}
	netConfigs, err := loadNetConfigs(args)
	if err != nil {
		return
	}
	for i, netConf := range netConfigs {
		// TODO: Check that/when/how /etc/lib/cni/networks/<net>/last_reserved_ip is reset
		if e := mngr.DelNet("cni"+strconv.Itoa(i), netConf); e != nil && err == nil {
			err = e
		}
	}
	return
}

func applyArgs(cfg *net.ConfigFileGenerator) {
	if flagHostname != "" {
		cfg.SetHostname(flagHostname)
	}
	if flagDomainname != "" {
		cfg.SetDnsDomain(flagDomainname)
	}
	for _, e := range flagHostsEntries {
		cfg.AddHostsEntry(e.name, e.ip)
	}
	cfg.AddDnsNameserver(flagDns)
	cfg.AddDnsSearch(flagDnsSearch)
	cfg.AddDnsOptions(flagDnsOptions)
}

func loadNetConfigs(args []string) (r []*libcni.NetworkConfigList, err error) {
	netConfPath, err := getNetConfPath()
	if err != nil {
		return
	}
	if len(args) == 0 && len(flagPorts) > 0 {
		return nil, errors.New("Cannot publish a port without a container network! Please remove the --publish option or add --network")
	}
	networks := net.NewNetConfigs(netConfPath)
	r = make([]*libcni.NetworkConfigList, len(args))
	for i, name := range args {
		cfg, err := networks.GetConfig(name)
		if err != nil {
			return nil, err
		}
		if i == 0 {
			// Apply port mapping to 1st network
			cfg, err = net.MapPorts(cfg, flagPorts)
			if err != nil {
				return nil, err
			}
		}
		r[i] = cfg
	}
	return
}

func getNetConfPath() (confDir string, err error) {
	if confDir = os.Getenv("NETCONFPATH"); confDir == "" {
		var homeDir string
		homeDir, err = homedir.Dir()
		if err != nil {
			return "", errors.Wrap(err, "derive NETCONFPATH from home dir")
		}
		confDir = filepath.Join(homeDir, ".cni/net.d")
		if _, e := os.Stat(confDir); e != nil {
			// fall back to global cni conf dir when user conf dir does not exist
			confDir = "/etc/cni/net.d"
		}
	} else if confDir, err = homedir.Expand(confDir); err != nil {
		err = errors.Wrap(err, "expand NETCONFPATH")
	}
	return
}

func readContainerState() (s *specs.State, err error) {
	s = &specs.State{}
	// Read hook data from stdin
	b, err := ioutil.ReadAll(os.Stdin)
	if err != nil {
		return nil, errors.Wrap(err, "read OCI state from stdin")
	}

	// Unmarshal the hook state
	if err = json.Unmarshal(b, s); err != nil {
		err = errors.Wrap(err, "unmarshal OCI state read from stdin")
	}
	return
}

func loadBundleSpec(s *specs.State) (*specs.Spec, error) {
	spec := &specs.Spec{}
	f, err := os.Open(filepath.Join(s.Bundle, "config.json"))
	if err != nil {
		return nil, errors.Wrap(err, "open runtime bundle spec")
	}
	b, err := ioutil.ReadAll(f)
	if err != nil {
		return nil, errors.Wrap(err, "read runtime bundle spec")
	}
	if err = json.Unmarshal(b, spec); err != nil {
		return nil, errors.Wrap(err, "unmarshal runtime bundle spec")
	}

	return spec, nil
}
