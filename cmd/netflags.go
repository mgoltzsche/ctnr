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
	"strings"

	"github.com/mgoltzsche/cntnr/net"
	"github.com/spf13/pflag"
)

var (
	flagHostname     string
	flagDomainname   string
	flagDns          []string
	flagDnsSearch    []string
	flagDnsOptions   []string
	flagHostsEntries []hostsEntry
	flagPorts        []net.PortMapEntry
)

func initPortBindFlags(f *pflag.FlagSet) {
	f.VarP((*fPortBinding)(&flagPorts), "publish", "p", "container ports to be published on the host: [[HOSTIP:]HOSTPORT:]PORT[/PROT]")
}

func initNetFlags(f *pflag.FlagSet) {
	f.StringVar(&flagHostname, "hostname", "", "container hostname")
	f.StringVar(&flagDomainname, "domainname", "", "container domainname")
	f.StringSliceVar(&flagDns, "dns", nil, "DNS nameservers to write in container's /etc/resolv.conf")
	f.StringSliceVar(&flagDnsSearch, "dns-search", nil, "DNS search domains to write in container's /etc/resolv.conf")
	f.StringSliceVar(&flagDnsOptions, "dns-opts", nil, "DNS search options to write in container's /etc/resolv.conf")
	f.Var((*fExtraHosts)(&flagHostsEntries), "hosts-entry", "additional entries to write in container's /etc/hosts")
	initPortBindFlags(f)
}

type hostsEntry struct {
	name string
	ip   string
}

type fExtraHosts []hostsEntry

func (c *fExtraHosts) Set(v string) error {
	s := strings.SplitN(v, "=", 2)
	k := strings.Trim(s[0], " ")
	if len(s) != 2 || k == "" || strings.Trim(s[1], " ") == "" {
		return fmt.Errorf("Expected option value format: NAME=IP")
	}
	*c = append(*c, hostsEntry{k, strings.Trim(s[1], " ")})
	return nil
}

func (c *fExtraHosts) Type() string {
	return "NAME=IP..."
}

func (c *fExtraHosts) String() string {
	s := ""
	for _, e := range *c {
		s += strings.Trim(" "+e.name+"="+e.ip, " ")
	}
	return s
}

type fPortBinding []net.PortMapEntry

func (c *fPortBinding) Set(s string) error {
	return net.ParsePortMapping(s, (*[]net.PortMapEntry)(c))
}

func (c *fPortBinding) Type() string {
	return "port..."
}

func (c *fPortBinding) String() string {
	s := ""
	p := ([]net.PortMapEntry)(*c)
	if p != nil {
		for _, p := range p {
			s += strings.Trim(" "+p.String(), " ")
		}
	}
	return s
}
