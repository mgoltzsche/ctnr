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

package builder

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"

	utils "github.com/mgoltzsche/ctnr/pkg/sliceutils"
	specs "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/opencontainers/runtime-tools/generate"
	"github.com/pkg/errors"
)

const ANNOTATION_HOOK_ARGS = "com.github.mgoltzsche.ctnr.bundle.hook.args"

type NetConfig struct {
	DnsNameserver []string          `json:"dns,omitempty"`
	DnsSearch     []string          `json:"dns_search,omitempty"`
	DnsOptions    []string          `json:"dns_options,omitempty"`
	Domainname    string            `json:"domainname,omitempty"`
	Hosts         map[string]string `json:"hosts,omitempty"`
	Networks      []string          `json:"networks,omitempty"`
	Ports         []PortMapEntry    `json:"ports,omitempty"`
	IPAMDataDir   string            `json:"dataDir,omitempty"`
}

type PortMapEntry struct {
	Target    uint16 `json:"target"`
	Published uint16 `json:"published,omitempty"`
	Protocol  string `json:"protocol,omitempty"`
	IP        string `json:"ip,omitempty"`
}

func (p PortMapEntry) String() string {
	var s string
	pub := p.Published
	if pub == 0 {
		pub = p.Target
	}
	if p.IP == "" {
		s = strconv.Itoa(int(pub)) + ":"
	} else {
		s = p.IP + ":" + strconv.Itoa(int(pub)) + ":"
	}
	s += strconv.Itoa(int(p.Target))
	if p.Protocol != "" && p.Protocol != "tcp" {
		s += "/" + p.Protocol
	}
	return s
}

type HookBuilder struct {
	hook NetConfig
}

func NewHookBuilderFromSpec(spec *specs.Spec) (b HookBuilder, err error) {
	if spec != nil && spec.Annotations != nil {
		if hookArgs := spec.Annotations[ANNOTATION_HOOK_ARGS]; hookArgs != "" {
			if err = json.Unmarshal([]byte(hookArgs), &b.hook); err != nil {
				err = errors.Wrap(err, "hook builder from spec: read spec's hook args")
			}
		}
	}
	return
}

func (b *HookBuilder) SetIPAMDataDir(ipamDataDir string) {
	b.hook.IPAMDataDir = ipamDataDir
}

func (b *HookBuilder) SetDomainname(domainname string) {
	b.hook.Domainname = domainname
}

func (b *HookBuilder) AddDnsNameserver(nameserver string) {
	utils.AddToSet(&b.hook.DnsNameserver, nameserver)
}

func (b *HookBuilder) AddDnsSearch(search string) {
	utils.AddToSet(&b.hook.DnsSearch, search)
}

func (b *HookBuilder) AddDnsOption(opt string) {
	utils.AddToSet(&b.hook.DnsOptions, opt)
}

func (b *HookBuilder) AddHost(host, ip string) {
	if b.hook.Hosts == nil {
		b.hook.Hosts = map[string]string{}
	}
	b.hook.Hosts[host] = ip
}

func (b *HookBuilder) AddNetwork(networkID string) {
	utils.AddToSet(&b.hook.Networks, networkID)
}

func (b *HookBuilder) AddPortMapEntry(entry PortMapEntry) {
	b.hook.Ports = append(b.hook.Ports, entry)
}

func (b *HookBuilder) Build(spec *generate.Generator) (err error) {
	defer func() {
		if err != nil {
			err = errors.Wrap(err, "generate hook call")
		}
	}()

	//hookBinary, err := exec.LookPath("ctnr-hooks")
	executable, err := os.Executable()
	if err != nil {
		return errors.Wrap(err, "find network hook binary")
	}
	cniPluginPaths := os.Getenv("CNI_PATH")
	if cniPluginPaths == "" {
		cniPluginPaths = filepath.Join(filepath.Dir(executable), "..", "cni-plugins")
		if s, err := os.Stat(cniPluginPaths); err != nil || !s.IsDir() {
			return errors.New("CNI plugin directory cannot be derived from executable (../cni-plugins) and CNI_PATH env var is not specified. See https://github.com/containernetworking/cni/blob/master/SPEC.md")
		}
	}
	cniEnv := []string{
		"PATH=" + os.Getenv("PATH"),
		"CNI_PATH=" + cniPluginPaths,
	}
	if netConfPath := os.Getenv("NETCONFPATH"); netConfPath != "" {
		cniEnv = append(cniEnv, "NETCONFPATH="+netConfPath)
	}
	ipamDataDir := b.hook.IPAMDataDir
	if ipamDataDir == "" {
		ipamDataDir = os.Getenv("IPAMDATADIR")
	}
	if ipamDataDir != "" {
		cniEnv = append(cniEnv, "IPAMDATADIR="+ipamDataDir)
	}

	netInitHookArgs := make([]string, 0, 10)
	netInitHookArgs = append(netInitHookArgs, "ctnr", "net", "init")
	netRmHookArgs := make([]string, 0, 5)
	netRmHookArgs = append(netRmHookArgs, "ctnr", "net", "rm")
	if b.hook.Domainname != "" {
		netInitHookArgs = append(netInitHookArgs, "--domainname="+b.hook.Domainname)
	}
	for _, nameserver := range b.hook.DnsNameserver {
		netInitHookArgs = append(netInitHookArgs, "--dns="+nameserver)
	}
	for _, search := range b.hook.DnsSearch {
		netInitHookArgs = append(netInitHookArgs, "--dns-search="+search)
	}
	for _, opt := range b.hook.DnsOptions {
		netInitHookArgs = append(netInitHookArgs, "--dns-opts="+opt)
	}
	for name, ip := range b.hook.Hosts {
		netInitHookArgs = append(netInitHookArgs, "--hosts-entry="+name+"="+ip)
	}
	for _, p := range b.hook.Ports {
		pOpt := "--publish=" + p.String()
		netInitHookArgs = append(netInitHookArgs, pOpt)
		netRmHookArgs = append(netRmHookArgs, pOpt)
	}
	if len(b.hook.Networks) > 0 {
		netInitHookArgs = append(netInitHookArgs, b.hook.Networks...)
		netRmHookArgs = append(netRmHookArgs, b.hook.Networks...)
	}

	// Add hooks
	spec.ClearPreStartHooks()
	spec.ClearPostStopHooks()
	spec.AddPreStartHook(executable, netInitHookArgs)
	spec.AddPreStartHookEnv(executable, cniEnv)

	if len(b.hook.Networks) > 0 {
		spec.AddPostStopHook(executable, netRmHookArgs)
		spec.AddPostStopHookEnv(executable, cniEnv)
	}

	// Add hook args metadata as annotation to parse it when it should be modified
	j, err := json.Marshal(b.hook)
	if err != nil {
		return
	}
	spec.AddAnnotation(ANNOTATION_HOOK_ARGS, string(j))
	return
}
