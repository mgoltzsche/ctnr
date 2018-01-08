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
	"path/filepath"
	"strings"

	"github.com/mgoltzsche/cntnr/model"
	"github.com/mgoltzsche/cntnr/net"
	"github.com/spf13/pflag"
)

var flagsBundle = newApps()

func initBundleRunFlags(f *pflag.FlagSet) {
	f.BoolVarP(&flagsBundle.stdin, "stdin", "i", false, "binds stdin to the container")
}

func initNetConfFlags(f *pflag.FlagSet, c *netCfg) {
	f.Var((*cHostname)(c), "hostname", "container hostname")
	f.Var((*cDomainname)(c), "domainname", "container domainname")
	f.Var((*cDns)(c), "dns", "DNS nameservers to write in container's /etc/resolv.conf")
	f.Var((*cDnsSearch)(c), "dns-search", "DNS search domains to write in container's /etc/resolv.conf")
	f.Var((*cDnsOptions)(c), "dns-opts", "DNS search options to write in container's /etc/resolv.conf")
	f.Var((*cExtraHosts)(c), "hosts-entry", "additional entries to write in container's /etc/hosts")
	f.VarP((*cPortBinding)(c), "publish", "p", "container ports to be published on the host: [[HOSTIP:]HOSTPORT:]PORT[/PROT]")
	f.Var((*cNetworks)(c), "network", "add CNI network to container's network namespace")
}

func newApps() *apps {
	f := &apps{}
	return f
}

type apps struct {
	netCfg
	// TODO: update
	//Update   bool
	stdin    bool
	tty      bool
	readonly bool
	app      *model.Service
}

func (c *apps) InitFlags(f *pflag.FlagSet) {
	f.Var((*cName)(c), "name", "container name. Also used as hostname when hostname is not set explicitly")
	f.Var((*cEntrypoint)(c), "entrypoint", "container entrypoint")
	f.VarP((*cWorkingDir)(c), "workdir", "w", "container entrypoint")
	f.VarP((*cEnvironment)(c), "env", "e", "container environment variables")
	f.Var((*cCapAdd)(c), "cap-add", "add process capability ('all' adds all)")
	f.Var((*cCapDrop)(c), "cap-drop", "drop process capability ('all' drops all)")
	f.Var((*cSeccomp)(c), "seccomp", "seccomp profile file or 'default' or 'unconfined'")
	f.Var((*cMountCgroups)(c), "mount-cgroups", "Mounts the host's cgroups with the given option: ro|rw|no")
	f.Var((*cVolumeMount)(c), "mount", "container volume mounts: TARGET|SOURCE:TARGET[:OPTIONS]")
	f.Var((*cExpose)(c), "expose", "container ports to be exposed")
	f.BoolVar(&c.readonly, "readonly", false, "mounts the root file system in read only mode")
	f.BoolVarP(&c.tty, "tty", "t", false, "binds a terminal to the container")
	//f.BoolVarP(&c.Update, "update", "u", false, "Updates an existing bundle's configuration and rootfs if changed")
	initNetConfFlags(f, &c.netCfg)
	// Stop parsing after first non flag argument (image)
	f.SetInterspersed(false)
}

func (c *apps) last() *model.Service {
	if c.app == nil {
		c.app = model.NewService("")
	}
	return c.app
}

func (c *apps) Get() (*model.Service, error) {
	if c.app == nil {
		return nil, usageError("No service defined")
	}
	s := c.app
	s.NetConf = c.net
	s.Tty = c.tty
	s.StdinOpen = c.stdin
	s.ReadOnly = c.readonly
	c.app = nil
	c.net = model.NetConf{}
	return s, nil
}

func (c *apps) SetBundleArgs(ca []string) error {
	if len(ca) == 0 {
		return usageError("No image arg specified")
	}
	last := c.last()
	last.Image = ca[0]
	if len(ca) > 1 {
		last.Command = ca[1:]
	}
	return nil
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

type cEntrypoint apps

func (c *cEntrypoint) Set(s string) error {
	(*apps)(c).last().Entrypoint = nil
	return addStringEntries(s, &(*apps)(c).last().Entrypoint)
}

func (c *cEntrypoint) Type() string {
	return "cmd"
}

func (c *cEntrypoint) String() string {
	return entriesToString((*apps)(c).last().Entrypoint)
}

type cWorkingDir apps

func (c *cWorkingDir) Set(s string) error {
	(*apps)(c).last().Cwd = s
	return nil
}

func (c *cWorkingDir) Type() string {
	return "dir"
}

func (c *cWorkingDir) String() string {
	return (*apps)(c).last().Cwd
}

type cEnvironment apps

func (c *cEnvironment) Set(s string) error {
	return addMapEntries(s, &(*apps)(c).last().Environment)
}

func (c *cEnvironment) Type() string {
	return "NAME=VALUE..."
}

func (c *cEnvironment) String() string {
	return mapToString((*apps)(c).last().Environment)
}

type cCapAdd apps

func (c *cCapAdd) Set(s string) error {
	return addStringEntries(s, &(*apps)(c).last().CapAdd)
}

func (c *cCapAdd) Type() string {
	return "string..."
}

func (c *cCapAdd) String() string {
	return entriesToString((*apps)(c).last().CapAdd)
}

type cCapDrop apps

func (c *cCapDrop) Set(s string) error {
	if strings.ToUpper(s) == "ALL" {
		(*apps)(c).last().CapAdd = nil
		return nil
	} else {
		return addStringEntries(s, &(*apps)(c).last().CapDrop)
	}
}

func (c *cCapDrop) Type() string {
	return "string..."
}

func (c *cCapDrop) String() string {
	return entriesToString((*apps)(c).last().CapDrop)
}

type cSeccomp apps

func (c *cSeccomp) Set(s string) error {
	(*apps)(c).last().Seccomp = s
	return nil
}

func (c *cSeccomp) Type() string {
	return "string"
}

func (c *cSeccomp) String() string {
	return (*apps)(c).last().Seccomp
}

type cMountCgroups apps

func (c *cMountCgroups) Set(opt string) error {
	(*apps)(c).last().MountCgroups = opt
	return nil
}

func (c *cMountCgroups) Type() string {
	return "string"
}

func (c *cMountCgroups) String() string {
	return (*apps)(c).last().MountCgroups
}

type cExpose apps

func (c *cExpose) Set(s string) (err error) {
	return addStringEntries(s, &(*apps)(c).last().Expose)
}

func (c *cExpose) Type() string {
	return "port..."
}

func (c *cExpose) String() string {
	return entriesToString((*apps)(c).last().Entrypoint)
}

type cVolumeMount apps

func (c *cVolumeMount) Set(s string) (err error) {
	v := model.VolumeMount{}
	if err = model.ParseVolumeMount(s, &v); err != nil {
		return
	}
	v.Source, err = filepath.Abs(v.Source)
	if err != nil {
		return
	}
	r := &(*apps)(c).last().Volumes
	*r = append(*r, v)
	return
}

func (c *cVolumeMount) Type() string {
	return "string..."
}

func (c *cVolumeMount) String() string {
	s := ""
	for _, v := range (*apps)(c).last().Volumes {
		s += strings.Trim(" "+v.String(), " ")
	}
	return s
}

type netCfg struct {
	net model.NetConf
}

type cHostname netCfg

func (c *cHostname) Set(s string) error {
	(*netCfg)(c).net.Hostname = s
	return nil
}

func (c *cHostname) Type() string {
	return "string"
}

func (c *cHostname) String() string {
	return (*netCfg)(c).net.Hostname
}

type cDomainname netCfg

func (c *cDomainname) Set(s string) error {
	(*netCfg)(c).net.Domainname = s
	return nil
}

func (c *cDomainname) Type() string {
	return "string"
}

func (c *cDomainname) String() string {
	return (*netCfg)(c).net.Domainname
}

type cDns netCfg

func (c *cDns) Set(s string) error {
	return addStringEntries(s, &(*netCfg)(c).net.Dns)
}

func (c *cDns) Type() string {
	return "string..."
}

func (c *cDns) String() string {
	return entriesToString((*netCfg)(c).net.Dns)
}

type cDnsSearch netCfg

func (c *cDnsSearch) Set(s string) error {
	return addStringEntries(s, &(*netCfg)(c).net.DnsSearch)
}

func (c *cDnsSearch) Type() string {
	return "string..."
}

func (c *cDnsSearch) String() string {
	return entriesToString((*netCfg)(c).net.DnsSearch)
}

type cDnsOptions netCfg

func (c *cDnsOptions) Set(s string) error {
	return addStringEntries(s, &(*netCfg)(c).net.DnsOptions)
}

func (c *cDnsOptions) Type() string {
	return "string..."
}

func (c *cDnsOptions) String() string {
	return entriesToString((*netCfg)(c).net.DnsOptions)
}

type cExtraHosts netCfg

func (c *cExtraHosts) Set(v string) error {
	s := strings.SplitN(v, "=", 2)
	k := strings.Trim(s[0], " ")
	if len(s) != 2 || k == "" || strings.Trim(s[1], " ") == "" {
		return fmt.Errorf("Expected option value format: NAME=IP")
	}
	(*c).net.ExtraHosts = append((*c).net.ExtraHosts, model.ExtraHost{k, strings.Trim(s[1], " ")})
	return nil
}

func (c *cExtraHosts) Type() string {
	return "NAME=IP..."
}

func (c *cExtraHosts) String() string {
	s := ""
	for _, e := range (*c).net.ExtraHosts {
		s += strings.Trim(" "+e.Name+"="+e.Ip, " ")
	}
	return s
}

type cPortBinding netCfg

func (c *cPortBinding) Set(s string) (err error) {
	ports := make([]net.PortMapEntry, 0, 1)
	if err = net.ParsePortMapping(s, &ports); err != nil {
		return
	}
	for _, p := range ports {
		(*netCfg)(c).net.Ports = append((*netCfg)(c).net.Ports, model.PortBinding{
			Published: p.HostPort,
			Target:    p.ContainerPort,
			Protocol:  p.Protocol,
			IP:        p.HostIP,
		})
	}
	return
}

func (c *cPortBinding) Type() string {
	return "port..."
}

func (c *cPortBinding) String() string {
	s := ""
	p := (*netCfg)(c).net.Ports
	if p != nil {
		for _, p := range p {
			s += strings.Trim(" "+p.String(), " ")
		}
	}
	return s
}

type cNetworks netCfg

func (c *cNetworks) Set(s string) error {
	return addStringEntries(s, &(*netCfg)(c).net.Networks)
}

func (c *cNetworks) Type() string {
	return "string..."
}

func (c *cNetworks) String() string {
	return entriesToString((*netCfg)(c).net.Networks)
}
