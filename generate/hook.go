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

package generate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	utils "github.com/mgoltzsche/cntnr/pkg/sliceutils"
	specs "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/opencontainers/runtime-tools/generate"
)

const ANNOTATION_HOOK_ARGS = "com.github.mgoltzsche.cntnr.bundle.hook.args"

type NetConfig struct {
	DnsNameserver []string          `json:"dns,omitempty"`
	DnsSearch     []string          `json:"dns_search,omitempty"`
	DnsOptions    []string          `json:"dns_options,omitempty"`
	Domainname    string            `json:"domainname,omitempty"`
	Hosts         map[string]string `json:"hosts,omitempty"`
	Networks      []string          `json:"networks,omitempty"`
	Ports         []PortMapEntry    `json:"ports,omitempty"`
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
				err = fmt.Errorf("hook builder from spec: read spec's hook args: %s", err)
			}
		}
	}
	return
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
			err = fmt.Errorf("generate hook call: %s", err)
		}
	}()

	//hookBinary, err := exec.LookPath("cntnr-hooks")
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("Cannot find network hook binary! %s", err)
	}
	cniPluginPaths := os.Getenv("CNI_PATH")
	if cniPluginPaths == "" {
		pluginPath := filepath.Join(filepath.Dir(executable), "..", "cni-plugins")
		if s, err := os.Stat(pluginPath); err == nil && s.IsDir() {
			cniPluginPaths = pluginPath
		}
	}
	if cniPluginPaths == "" {
		return fmt.Errorf("CNI_PATH environment variable empty. It must contain paths to CNI plugins. See https://github.com/containernetworking/cni/blob/master/SPEC.md")
	}
	// TODO: add all CNI env vars
	cniEnv := []string{
		"PATH=" + os.Getenv("PATH"),
		"CNI_PATH=" + cniPluginPaths,
	}

	hookArgs := make([]string, 0, 10)
	hookArgs = append(hookArgs, "cntnr", "net", "init")
	if b.hook.Domainname != "" {
		hookArgs = append(hookArgs, "--domainname="+b.hook.Domainname)
	}
	for _, nameserver := range b.hook.DnsNameserver {
		hookArgs = append(hookArgs, "--dns="+nameserver)
	}
	for _, search := range b.hook.DnsSearch {
		hookArgs = append(hookArgs, "--dns-search="+search)
	}
	for _, opt := range b.hook.DnsOptions {
		hookArgs = append(hookArgs, "--dns-opts="+opt)
	}
	for name, ip := range b.hook.Hosts {
		hookArgs = append(hookArgs, "--hosts-entry="+name+"="+ip)
	}
	for _, p := range b.hook.Ports {
		hookArgs = append(hookArgs, "--publish="+p.String())
	}
	if len(b.hook.Networks) > 0 {
		hookArgs = append(hookArgs, b.hook.Networks...)
	}

	// Add hooks
	spec.ClearPreStartHooks()
	spec.ClearPostStopHooks()
	spec.AddPreStartHook(executable, hookArgs)
	spec.AddPreStartHookEnv(executable, cniEnv)

	if len(b.hook.Networks) > 0 {
		spec.AddPostStopHook(executable, append([]string{"cntnr", "net", "rm"}, b.hook.Networks...))
		spec.AddPostStopHookEnv(executable, cniEnv)
	}

	// Add hook args metadata as annotation to parse it when it should be modified
	// TODO: better parse hook args directly by using same code the hook uses
	j, err := json.Marshal(b.hook)
	if err != nil {
		return
	}
	spec.AddAnnotation(ANNOTATION_HOOK_ARGS, string(j))
	return
}
