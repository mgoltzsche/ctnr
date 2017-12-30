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
	"strconv"
	"strings"

	shellwords "github.com/mattn/go-shellwords"
	//"github.com/mgoltzsche/cntnr/generate"
	"github.com/mgoltzsche/cntnr/model"
	"github.com/spf13/pflag"
)

var flagsBundle = newApps()

func initBundleRunFlags(f *pflag.FlagSet) {
	f.VarP((*cStdin)(flagsBundle), "stdin", "i", "binds stdin to the container")
}

func initBundleCreateFlags(f *pflag.FlagSet) {
	c := flagsBundle
	f.Var((*cName)(c), "name", "container name. Also used as hostname when hostname is not set explicitly")
	f.Var((*cEntrypoint)(c), "entrypoint", "container entrypoint")
	f.VarP((*cEnvironment)(c), "env", "e", "container environment variables")
	f.Var((*cVolumeMount)(c), "mount", "container volume mounts: TARGET|SOURCE:TARGET[:OPTIONS]")
	f.Var((*cExpose)(c), "expose", "container ports to be exposed")
	f.Var((*cReadOnly)(c), "readonly", "mounts the root file system in read only mode")
	f.VarP((*cTty)(flagsBundle), "tty", "t", "binds a terminal to the container")
	initNetConfFlags(f, &c.netCfg)
	// Stop parsing after first non flag argument (image)
	f.SetInterspersed(false)
}

func initNetConfFlags(f *pflag.FlagSet, c *netCfg) {
	f.Var((*cHostname)(c), "hostname", "container hostname")
	f.Var((*cDomainname)(c), "domainname", "container domainname")
	f.Var((*cDns)(c), "dns", "DNS nameservers to write in container's /etc/resolv.conf")
	f.Var((*cDnsSearch)(c), "dns-search", "DNS search domains to write in container's /etc/resolv.conf")
	f.Var((*cDnsOptions)(c), "dns-opts", "DNS search options to write in container's /etc/resolv.conf")
	f.Var((*cExtraHosts)(c), "hosts-entry", "additional entries to write in container's /etc/hosts")
	f.VarP((*cPortBinding)(c), "publish", "p", "container ports to be published on the host: [[HOSTIP:]HOSTPORT:]PORT[/PROT]")
	f.Var((*cNetworks)(c), "net", "add CNI network to container's network namespace")
}

func newApps() *apps {
	f := &apps{netCfg{nil}, []*model.Service{}}
	f.add()
	return f
}

/*type bundleFlags struct {
	flags []bundleFlag
}

func (f *bundleFlags) reset() {
	for _, e := range flags {
		e.reset()
	}
}

func (f *bundleFlags) apply(spec *generate.SpecBuilder) {
	for _, e := range flags {
		e.apply(spec)
	}
}

type bundleFlag interface {
	resetValue()
	apply(*generate.SpecBuilder)
}

type stringFlag struct {
	value  string
	setter func(string)
}

func (f *stringFlag) resetValue() {
	f.value = ""
}*/

type apps struct {
	netCfg
	apps []*model.Service
}

func (c *apps) add() {
	s := model.NewService("")
	c.curr = &s.NetConf
	c.apps = append(c.apps, s)
}

func (c *apps) last() *model.Service {
	return c.apps[len(c.apps)-1]
}

func (c *apps) setBundleArgs(ca []string) error {
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

type cEntrypoint apps

func (c *cEntrypoint) Set(s string) (err error) {
	return addStringEntries(s, &(*apps)(c).last().Entrypoint)
}

func (c *cEntrypoint) Type() string {
	return "string..."
}

func (c *cEntrypoint) String() string {
	return entriesToString((*apps)(c).last().Entrypoint)
}

type cEnvironment apps

func (c *cEnvironment) Set(s string) (err error) {
	return addMapEntries(s, &(*apps)(c).last().Environment)
}

func (c *cEnvironment) Type() string {
	return "NAME=VALUE..."
}

func (c *cEnvironment) String() string {
	return mapToString((*apps)(c).last().Environment)
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
	curr *model.NetConf
}

type cHostname netCfg

func (c *cHostname) Set(s string) error {
	(*netCfg)(c).curr.Hostname = s
	return nil
}

func (c *cHostname) Type() string {
	return "string"
}

func (c *cHostname) String() string {
	return (*netCfg)(c).curr.Hostname
}

type cDomainname netCfg

func (c *cDomainname) Set(s string) error {
	(*netCfg)(c).curr.Domainname = s
	return nil
}

func (c *cDomainname) Type() string {
	return "string"
}

func (c *cDomainname) String() string {
	return (*netCfg)(c).curr.Domainname
}

type cDns netCfg

func (c *cDns) Set(s string) error {
	return addStringEntries(s, &(*netCfg)(c).curr.Dns)
}

func (c *cDns) Type() string {
	return "string..."
}

func (c *cDns) String() string {
	return entriesToString((*netCfg)(c).curr.Dns)
}

type cDnsSearch netCfg

func (c *cDnsSearch) Set(s string) error {
	return addStringEntries(s, &(*netCfg)(c).curr.DnsSearch)
}

func (c *cDnsSearch) Type() string {
	return "string..."
}

func (c *cDnsSearch) String() string {
	return entriesToString((*netCfg)(c).curr.DnsSearch)
}

type cDnsOptions netCfg

func (c *cDnsOptions) Set(s string) error {
	return addStringEntries(s, &(*netCfg)(c).curr.DnsOptions)
}

func (c *cDnsOptions) Type() string {
	return "string..."
}

func (c *cDnsOptions) String() string {
	return entriesToString((*netCfg)(c).curr.DnsOptions)
}

type cExtraHosts netCfg

func (c *cExtraHosts) Set(v string) error {
	s := strings.SplitN(v, "=", 2)
	k := strings.Trim(s[0], " ")
	if len(s) != 2 || k == "" || strings.Trim(s[1], " ") == "" {
		return fmt.Errorf("Expected option value format: NAME=IP")
	}
	(*c).curr.ExtraHosts = append((*c).curr.ExtraHosts, model.ExtraHost{k, strings.Trim(s[1], " ")})
	return nil
}

func (c *cExtraHosts) Type() string {
	return "NAME=IP..."
}

func (c *cExtraHosts) String() string {
	s := ""
	for _, e := range (*c).curr.ExtraHosts {
		s += strings.Trim(" "+e.Name+"="+e.Ip, " ")
	}
	return s
}

type cPortBinding netCfg

func (c *cPortBinding) Set(s string) error {
	return model.ParsePortBinding(s, &(*netCfg)(c).curr.Ports)
}

func (c *cPortBinding) Type() string {
	return "port..."
}

func (c *cPortBinding) String() string {
	s := ""
	p := (*netCfg)(c).curr.Ports
	if p != nil {
		for _, p := range p {
			s += strings.Trim(" "+p.String(), " ")
		}
	}
	return s
}

type cNetworks netCfg

func (c *cNetworks) Set(s string) error {
	return addStringEntries(s, &(*netCfg)(c).curr.Networks)
}

func (c *cNetworks) Type() string {
	return "string..."
}

func (c *cNetworks) String() string {
	return entriesToString((*netCfg)(c).curr.Networks)
}

func parseBool(s string) (bool, error) {
	b, err := strconv.ParseBool(s)
	if err != nil {
		err = fmt.Errorf("Only 'true' or 'false' are accepted values")
	}
	return b, err
}

func addStringEntries(s string, r *[]string) error {
	if s == "" {
		*r = nil
		return nil
	}
	// TODO: fix parsing of cat asdf | sed ... > asd
	// (currently parse ignores everything after the cat cmd silently)
	e, err := shellwords.Parse(s)
	if err != nil {
		return err
	}
	*r = append(*r, e...)
	return nil
}

func entriesToString(l []string) string {
	s := ""
	if len(l) > 0 {
		for _, e := range l {
			s += fmt.Sprintf(" %q", e)
		}
	}
	return strings.Trim(s, " ")
}

func addMapEntries(s string, r *map[string]string) error {
	entries, err := shellwords.Parse(s)
	if err != nil {
		return err
	}
	if *r == nil {
		*r = map[string]string{}
	}
	for _, e := range entries {
		sp := strings.SplitN(e, "=", 2)
		k := strings.Trim(sp[0], " ")
		if len(sp) != 2 || k == "" || strings.Trim(sp[1], " ") == "" {
			return fmt.Errorf("Expected option value format: NAME=VALUE")
		}
		(*r)[k] = strings.Trim(sp[1], " ")
	}
	return nil
}

func mapToString(m map[string]string) string {
	s := ""
	if len(m) > 0 {
		for k, v := range m {
			s += strings.Trim(fmt.Sprintf(" %q", k+"="+v), " ")
		}
	}
	return s
}
