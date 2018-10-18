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
	"os"
	"sort"
	"strings"
	"time"

	"github.com/mgoltzsche/ctnr/pkg/idutils"
	ispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/opencontainers/runc/libcontainer/specconv"
	rspecs "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/opencontainers/runtime-tools/generate"
	"github.com/opencontainers/runtime-tools/generate/seccomp"
	"github.com/pkg/errors"
	"github.com/syndtr/gocapability/capability"
)

type SpecBuilder struct {
	generate.Generator
	entrypoint []string
	cmd        []string
	user       idutils.User
	prootPath  string
	rootless   bool
}

func NewSpecBuilder() SpecBuilder {
	return SpecBuilder{Generator: generate.New()}
}

func FromSpec(spec *rspecs.Spec) SpecBuilder {
	return SpecBuilder{Generator: generate.NewFromSpec(spec)}
}

func (b *SpecBuilder) ToRootless() {
	specconv.ToRootless(b.Generator.Spec())
	b.rootless = true
}

func (b *SpecBuilder) UseHostNetwork() {
	b.RemoveLinuxNamespace(rspecs.NetworkNamespace)
	opts := []string{"bind", "mode=0444", "nosuid", "noexec", "nodev", "ro"}
	b.AddBindMount("/etc/hosts", "/etc/hosts", opts)
	b.AddBindMount("/etc/resolv.conf", "/etc/resolv.conf", opts)
}

func (b *SpecBuilder) SetProcessUser(user idutils.User) {
	b.user = user
}

func (b *SpecBuilder) AddAllProcessCapabilities() {
	// Add all capabilities
	all := capability.List()
	caps := make([]string, len(all))
	for i, c := range all {
		caps[i] = "CAP_" + strings.ToUpper(c.String())
	}
	c := b.Generator.Spec().Process.Capabilities
	c.Effective = caps
	c.Permitted = caps
	c.Bounding = caps
	c.Ambient = caps
	c.Inheritable = caps
}

func (b *SpecBuilder) DropAllProcessCapabilities() {
	caps := []string{}
	c := b.Generator.Spec().Process.Capabilities
	c.Effective = caps
	c.Permitted = caps
	c.Bounding = caps
	c.Ambient = caps
	c.Inheritable = caps
}

// Derives a sane default seccomp profile from the current spec.
// See https://github.com/jessfraz/blog/blob/master/content/post/how-to-use-new-docker-seccomp-profiles.md
// and https://github.com/jessfraz/docker/blob/52f32818df8bad647e4c331878fa44317e724939/docs/security/seccomp.md
func (b *SpecBuilder) SetLinuxSeccompDefault() {
	spec := b.Generator.Spec()
	spec.Linux.Seccomp = seccomp.DefaultProfile(spec)
}

func (b *SpecBuilder) SetLinuxSeccompUnconfined() {
	spec := b.Generator.Spec()
	profile := seccomp.DefaultProfile(spec)
	profile.DefaultAction = rspecs.ActAllow
	profile.Syscalls = nil
	spec.Linux.Seccomp = profile
}

func (b *SpecBuilder) SetLinuxSeccomp(profile *rspecs.LinuxSeccomp) {
	spec := b.Generator.Spec()
	if spec.Linux == nil {
		spec.Linux = &rspecs.Linux{}
	}
	spec.Linux.Seccomp = profile
}

func (b *SpecBuilder) AddExposedPorts(ports []string) {
	// Merge exposedPorts annotation
	exposedPortsAnn := ""
	spec := b.Generator.Spec()
	if spec.Annotations != nil {
		exposedPortsAnn = spec.Annotations["org.opencontainers.image.exposedPorts"]
	}
	exposed := map[string]bool{}
	if exposedPortsAnn != "" {
		for _, exposePortStr := range strings.Split(exposedPortsAnn, ",") {
			exposed[strings.Trim(exposePortStr, " ")] = true
		}
	}
	for _, e := range ports {
		exposed[strings.Trim(e, " ")] = true
	}
	if len(exposed) > 0 {
		exposecsv := make([]string, len(exposed))
		i := 0
		for k := range exposed {
			exposecsv[i] = k
			i++
		}
		sort.Strings(exposecsv)
		b.AddAnnotation("org.opencontainers.image.exposedPorts", strings.Join(exposecsv, ","))
	}
}

func (b *SpecBuilder) SetPRootPath(prootPath string) {
	b.prootPath = prootPath
	// This has been derived from https://github.com/AkihiroSuda/runrootless/blob/b9a7df0120a7fee15c0223fd0fbc8c3885edd9b3/bundle/spec.go
	b.AddTmpfsMount("/dev/proot", []string{"exec", "mode=755", "size=32256k"})
	b.AddBindMount(prootPath, "/dev/proot/proot", []string{"bind", "ro"})
	b.AddProcessEnv("PROOT_TMP_DIR", "/dev/proot")
	b.AddProcessEnv("PROOT_NO_SECCOMP", "1")
	b.AddProcessCapability("CAP_" + capability.CAP_SYS_PTRACE.String())
	b.applyEntrypoint()
	b.SetLinuxSeccompDefault()
}

func (b *SpecBuilder) SetProcessEntrypoint(v []string) {
	b.entrypoint = v
	b.cmd = nil
	b.applyEntrypoint()
}

func (b *SpecBuilder) SetProcessCmd(v []string) {
	b.cmd = v
	b.applyEntrypoint()
}

func (b *SpecBuilder) applyEntrypoint() {
	var args []string
	if b.entrypoint != nil || b.cmd != nil {
		if b.entrypoint != nil && b.cmd != nil {
			args = append(b.entrypoint, b.cmd...)
		} else if b.entrypoint != nil {
			args = b.entrypoint
		} else {
			args = b.cmd
		}
	} else {
		args = []string{}
	}
	if b.prootPath != "" {
		args = append([]string{"/dev/proot/proot", "-0"}, args...)
	}
	b.SetProcessArgs(args)
}

// See image to runtime spec conversion rules: https://github.com/opencontainers/image-spec/blob/master/conversion.md
func (b *SpecBuilder) ApplyImage(img *ispecs.Image) {
	cfg := &img.Config

	// User
	b.user = idutils.ParseUser(img.Config.User)

	// Entrypoint
	b.SetProcessEntrypoint(cfg.Entrypoint)
	b.SetProcessCmd(cfg.Cmd)

	// Env
	if len(cfg.Env) > 0 {
		for _, e := range cfg.Env {
			kv := strings.SplitN(e, "=", 2)
			k := kv[0]
			v := ""
			if len(kv) == 2 {
				v = kv[1]
			}
			b.AddProcessEnv(k, v)
		}
	}

	// Working dir
	if cfg.WorkingDir != "" {
		b.SetProcessCwd(cfg.WorkingDir)
	}

	// Annotations
	if cfg.Labels != nil {
		for k, v := range cfg.Labels {
			b.AddAnnotation(k, v)
		}
	}
	// TODO: extract annotations also from image index and manifest
	if img.Author != "" {
		b.AddAnnotation("org.opencontainers.image.author", img.Author)
	}
	if img.Created != nil && !time.Unix(0, 0).Equal(*img.Created) {
		b.AddAnnotation("org.opencontainers.image.created", (*img.Created).String())
	}
	if img.Config.StopSignal != "" {
		b.AddAnnotation("org.opencontainers.image.stopSignal", img.Config.StopSignal)
	}
	if cfg.ExposedPorts != nil {
		ports := make([]string, len(cfg.ExposedPorts))
		i := 0
		for k := range cfg.ExposedPorts {
			ports[i] = k
			i++
		}
		b.AddAnnotation("org.opencontainers.image.exposedPorts", strings.Join(ports, ","))
	}
}

func (b *SpecBuilder) Spec(rootfs string) (spec *rspecs.Spec, err error) {
	usr, err := b.user.Resolve(rootfs)
	if err != nil {
		return
	}
	if b.rootless && (usr.Uid != 0 || usr.Gid != 0) {
		return nil, errors.Errorf("rootless containers support UID/GID 0 only but %q provided", b.user.String())
	}
	if usr.Uid > 1<<32 {
		return nil, errors.Errorf("uid %d exceeds range", usr.Uid)
	}
	if usr.Gid > 1<<32 {
		return nil, errors.Errorf("gid %d exceeds range", usr.Gid)
	}
	b.SetProcessUID(uint32(usr.Uid))
	b.SetProcessGID(uint32(usr.Gid))
	// TODO: set additional gids
	sp := b.Generator.Spec()
	if b.rootless {
		b.ClearLinuxUIDMappings()
		b.ClearLinuxGIDMappings()
		b.AddLinuxUIDMapping(uint32(os.Geteuid()), uint32(usr.Uid), 1)
		b.AddLinuxGIDMapping(uint32(os.Getegid()), uint32(usr.Gid), 1)
	}
	return sp, nil
}

func containsNamespace(ns rspecs.LinuxNamespaceType, l []rspecs.LinuxNamespace) bool {
	for _, e := range l {
		if e.Type == ns {
			return true
		}
	}
	return false
}
