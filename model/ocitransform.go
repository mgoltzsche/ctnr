package model

import (
	"encoding/json"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/mgoltzsche/cntnr/generate"
	"github.com/mgoltzsche/cntnr/pkg/sliceutils"
	specs "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/pkg/errors"
)

const (
	ANNOTATION_BUNDLE_IMAGE_NAME = "com.github.mgoltzsche.cntnr.bundle.image.name"
	ANNOTATION_BUNDLE_CREATED    = "com.github.mgoltzsche.cntnr.bundle.created"
	ANNOTATION_BUNDLE_ID         = "com.github.mgoltzsche.cntnr.bundle.id"
)

func (service *Service) ToSpec(res ResourceResolver, rootless bool, prootPath string, spec *generate.SpecBuilder) (err error) {
	defer func() {
		err = errors.Wrap(err, "generate OCI bundle spec")
	}()

	if rootless {
		spec.ToRootless()
	}

	if err = service.Process.ToSpecProcess(prootPath, spec); err != nil {
		return
	}

	// Readonly rootfs, mounts
	spec.SetRootReadonly(service.ReadOnly)

	if err = toMounts(service.Volumes, res, spec); err != nil {
		return
	}

	if service.MountCgroups != "" {
		if err = spec.AddCgroupsMount(service.MountCgroups); err != nil {
			return
		}
	}

	// Annotations
	if service.StopSignal != "" {
		spec.AddAnnotation("org.opencontainers.image.stopSignal", service.StopSignal)
	}
	if service.Expose != nil {
		// Merge exposedPorts annotation
		exposedPortsAnn := ""
		if spec.Spec().Annotations != nil {
			exposedPortsAnn = spec.Spec().Annotations["org.opencontainers.image.exposedPorts"]
		}
		exposed := map[string]bool{}
		if exposedPortsAnn != "" {
			for _, exposePortStr := range strings.Split(exposedPortsAnn, ",") {
				exposed[strings.Trim(exposePortStr, " ")] = true
			}
		}
		for _, e := range service.Expose {
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
			spec.AddAnnotation("org.opencontainers.image.exposedPorts", strings.Join(exposecsv, ","))
		}
	}

	// Seccomp
	if service.Seccomp == "" || service.Seccomp == "default" {
		// Derive seccomp configuration (must be called as last)
		spec.SetLinuxSeccompDefault()
	} else if service.Seccomp == "unconfined" {
		// Do not restrict operations with seccomp
		spec.SetLinuxSeccompUnconfined()
	} else {
		// Use seccomp configuration from file
		var j []byte
		if j, err = ioutil.ReadFile(res.ResolveFile(service.Seccomp)); err != nil {
			return
		}
		seccomp := &specs.LinuxSeccomp{}
		if err = json.Unmarshal(j, seccomp); err != nil {
			return
		}
		spec.SetLinuxSeccomp(seccomp)
	}

	if !rootless {
		// Limit resources
		//spec.SetLinuxResourcesPidsLimit(32771)
		//spec.AddLinuxResourcesHugepageLimit("2MB", 9223372036854772000)
		// TODO: add options to limit memory, cpu and blockIO access

		/*// Add network priority
		spec.Linux.Resources.Network.ClassID = ""
		spec.Linux.Resources.Network.Priorities = []specs.LinuxInterfacePriority{
			{"eth0", 2},
			{"lo", 1},
		}*/
	}

	// Init network IDs or host mode
	networks := service.Networks
	useNoNetwork := sliceutils.Contains(networks, "none")
	useHostNetwork := sliceutils.Contains(networks, "host")
	if (useNoNetwork || useHostNetwork) && len(networks) > 1 {
		return errors.New("transform: multiple networks are not supported when 'host' or 'none' network supplied")
	}
	if len(networks) == 0 {
		if rootless {
			networks = []string{}
			useHostNetwork = true
		} else {
			networks = []string{"default"}
		}
	} else if useNoNetwork || useHostNetwork {
		networks = []string{}
	} else if rootless {
		return errors.New("transform: no networks supported in rootless mode")
	}

	// Use host networks by removing 'network' namespace
	if useHostNetwork {
		spec.UseHostNetwork()
	}

	// Add hostname. Empty string results in host's hostname
	if service.Hostname != "" || useHostNetwork {
		spec.SetHostname(service.Hostname)
	}

	// Add network hook
	if len(networks) > 0 {
		hook, err := generate.NewHookBuilderFromSpec(spec.Spec())
		if err != nil {
			return err
		}
		for _, net := range networks {
			hook.AddNetwork(net)
		}
		if service.Domainname != "" {
			hook.SetDomainname(service.Domainname)
		}
		for _, dnsip := range service.Dns {
			hook.AddDnsNameserver(dnsip)
		}
		for _, search := range service.DnsSearch {
			hook.AddDnsSearch(search)
		}
		for _, opt := range service.DnsOptions {
			hook.AddDnsOption(opt)
		}
		for _, e := range service.ExtraHosts {
			hook.AddHost(e.Name, e.Ip)
		}
		for _, p := range service.Ports {
			hook.AddPortMapEntry(generate.PortMapEntry{
				Target:    p.Target,
				Published: p.Published,
				Protocol:  p.Protocol,
				IP:        p.IP,
			})
		}
		if err = hook.Build(&spec.Generator); err != nil {
			return err
		}
	} else if len(service.Ports) > 0 {
		return errors.New("transform: port mapping only supported with container network - add network or remove port mapping")
	}
	// TODO: register healthcheck (as Hook)
	return nil
}

func copyHostFile(file, rootDir string) error {
	b, err := ioutil.ReadFile(file)
	if err != nil {
		return err
	}
	err = ioutil.WriteFile(filepath.Join(rootDir, file), b, 0644)
	if err != nil {
		return err
	}
	return nil
}

func mountHostFile(spec *specs.Spec, file string) error {
	src := file
	fi, err := os.Lstat(file)
	if err != nil {
		return err
	}

	if fi.Mode()&os.ModeSymlink != 0 {
		src, err = os.Readlink(file)
		if err != nil {
			return err
		}
		if !filepath.IsAbs(src) {
			src = filepath.Join(filepath.Dir(file), src)
		}
	}

	spec.Mounts = append(spec.Mounts, specs.Mount{
		Type:        "bind",
		Source:      src,
		Destination: file,
		Options:     []string{"bind", "nodev", "mode=0444", "ro"},
	})
	return nil
}

func (p *Process) ToSpecProcess(prootPath string, spec *generate.SpecBuilder) (err error) {
	// Entrypoint & command
	if p.Entrypoint != nil {
		spec.SetProcessEntrypoint(p.Entrypoint)
		spec.SetProcessCmd([]string{})
	}
	if p.Command != nil {
		spec.SetProcessCmd(p.Command)
	}
	// Add proot
	if p.PRoot {
		if prootPath == "" {
			return errors.New("proot enabled but no proot path configured")
		}
		spec.SetPRootPath(prootPath)
	}

	// Env
	for k, v := range p.Environment {
		spec.AddProcessEnv(k, v)
	}

	// Working dir
	if p.Cwd != "" {
		spec.SetProcessCwd(p.Cwd)
	}

	// Terminal
	spec.SetProcessTerminal(p.Tty)

	// User
	if p.User != nil {
		if p.User.User != "" {
			// TODO: eventually map username using rootfs/etc/passwd to uid/gid
			//       (not possible here since filesystem is not yet populated. => Could be moved into bundle builder)
			usr, e := strconv.Atoi(p.User.User)
			if e == nil && usr >= 0 && usr < (1<<32) {
				spec.SetProcessUID(uint32(usr))
			} else {
				return errors.Errorf("uid expected but was %q", p.User.User)
			}
			if p.User.Group != "" {
				grp, e := strconv.Atoi(p.User.Group)
				if e == nil && grp >= 0 && grp < (1<<32) {
					spec.SetProcessGID(uint32(grp))
				} else {
					return errors.Errorf("gid expected but was %q", p.User.Group)
				}
			}
		}
		if p.User.AdditionalGroups != nil {
			for _, gidstr := range p.User.AdditionalGroups {
				gid, err := strconv.Atoi(gidstr)
				if err != nil || gid < 0 || gid > 1<<32 {
					return errors.Errorf("additional gid expected but was %q", gidstr)
				}
				spec.AddProcessAdditionalGid(uint32(gid))
			}
		}
	}

	// Capabilities
	if p.CapAdd != nil {
		for _, addCap := range p.CapAdd {
			if strings.ToUpper(addCap) == "ALL" {
				spec.AddAllProcessCapabilities()
				break
			} else if err = spec.AddProcessCapability("CAP_" + addCap); err != nil {
				return
			}
		}
		for _, dropCap := range p.CapDrop {
			if err = spec.DropProcessCapability("CAP_" + dropCap); err != nil {
				return
			}
		}
	}

	spec.SetProcessApparmorProfile(p.ApparmorProfile)
	spec.SetProcessNoNewPrivileges(p.NoNewPrivileges)
	spec.SetProcessSelinuxLabel(p.SelinuxLabel)
	if p.OOMScoreAdj != nil {
		spec.SetProcessOOMScoreAdj(*p.OOMScoreAdj)
	}

	return nil
}

func toMounts(mounts []VolumeMount, res ResourceResolver, spec *generate.SpecBuilder) error {
	for _, m := range mounts {
		src, err := res.ResolveMountSource(m)
		if err != nil {
			return err
		}

		t := m.Type
		if t == "" || t == MOUNT_TYPE_VOLUME {
			t = MOUNT_TYPE_BIND
		}
		opts := m.Options
		if len(opts) == 0 {
			// Apply default mount options. See man7.org/linux/man-pages/man8/mount.8.html
			opts = []string{"bind", "nodev", "mode=0755"}
		} else {
			sliceutils.AddToSet(&opts, "rbind")
		}

		spec.Spec().Mounts = append(spec.Spec().Mounts, specs.Mount{
			Type:        string(t),
			Source:      src,
			Destination: m.Target,
			Options:     opts,
		})
	}
	return nil
}
