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
	"fmt"
	"github.com/mgoltzsche/cntnr/model"
	"github.com/mgoltzsche/cntnr/net"
	specs "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/spf13/cobra"
	"io/ioutil"
	"os"
	"strconv"
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
		Run: handleError(runNetInit),
	}
	netRemoveCmd = &cobra.Command{
		Use:   "rm",
		Short: "Removes container networks",
		Long: `Removes a container's networks.
The OCI container state JSON is expected on stdin.
See OCI state spec at https://github.com/opencontainers/runtime-spec/blob/master/runtime.md`,
		Run: handleError(runNetRemove),
	}
	initFlags = &netCfg{&model.NetConf{}}
)

func init() {
	netCmd.AddCommand(netInitCmd)
	netCmd.AddCommand(netRemoveCmd)

	f := netInitCmd.Flags()
	initNetConfFlags(f, initFlags)
}

func runNetInit(cmd *cobra.Command, args []string) (err error) {
	netMan := containerNetworkManager()
	// TODO: Expose pub interface as CLI option
	pubIf := ""
	if len(args) > 0 {
		pubIf = "eth0"
	} else {
		pubIf = "lo"
	}
	for i, n := range args {
		if _, err = netMan.AddNet("eth"+strconv.Itoa(i), n); err != nil {
			return
		}
	}
	c := initFlags.curr
	if c.Domainname != "" {
		netMan.SetDomainname(c.Domainname)
	}
	if c.Hostname != "" {
		netMan.SetHostname(c.Hostname)
	}
	err = netMan.AddHostnameHostsEntry(pubIf)
	if err != nil {
		return
	}
	for _, e := range c.ExtraHosts {
		netMan.AddHostsEntry(e.Name, e.Ip)
	}
	netMan.AddDNS(net.DNS{
		c.Dns,
		c.DnsSearch,
		c.DnsOptions,
		c.Domainname,
	})
	return netMan.Apply()
}

func runNetRemove(cmd *cobra.Command, args []string) (err error) {
	// TODO: Make sure all network resources are removed properly since currently IP/interface stay reserved
	netMan := containerNetworkManager()
	for i, n := range args {
		if e := netMan.DelNet("eth"+strconv.Itoa(i), n); e != nil && err == nil {
			err = e
		}
	}
	return
}

func containerNetworkManager() *net.ContainerNetManager {
	m, err := net.NewContainerNetManager(readContainerState())
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cannot create container network manager: %s", err)
		os.Exit(1)
	}
	return m
}

func readContainerState() *specs.State {
	state := &specs.State{}
	// Read hook data from stdin
	b, err := ioutil.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cannot read OCI state from stdin: %v\n", err)
		os.Exit(1)
	}

	// Unmarshal the hook state
	if err := json.Unmarshal(b, state); err != nil {
		fmt.Fprintf(os.Stderr, "Cannot unmarshal OCI state from stdin: %v\n", err)
		os.Exit(1)
	}

	return state
}
