package oci

import (
	"encoding/json"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/mgoltzsche/ctnr/bundle/builder"
	"github.com/mgoltzsche/ctnr/model"
	"github.com/mgoltzsche/ctnr/pkg/idutils"
	"github.com/mgoltzsche/ctnr/pkg/sliceutils"
	specs "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/pkg/errors"
)

const (
	ANNOTATION_BUNDLE_IMAGE_NAME = "com.github.mgoltzsche.ctnr.bundle.image.name"
	ANNOTATION_BUNDLE_CREATED    = "com.github.mgoltzsche.ctnr.bundle.created"
	ANNOTATION_BUNDLE_ID         = "com.github.mgoltzsche.ctnr.bundle.id"
)

func ToSpec(service *model.Service, res model.ResourceResolver, rootless bool, ipamDataDir string, prootPath string, spec *builder.BundleBuilder) (err error) {
	defer func() {
		err = errors.Wrap(err, "generate OCI bundle spec")
	}()

	if rootless {
		spec.ToRootless()
	}

	sp := spec.Generator.Spec()

	if err = ToSpecProcess(&service.Process, prootPath, spec.SpecBuilder); err != nil {
		return
	}

	// Readonly rootfs, mounts
	spec.SetRootReadonly(service.ReadOnly)

	if err = toMounts(service.Volumes, res, spec); err != nil {
		return
	}

	// privileged
	seccomp := service.Seccomp
	cgroupsMount := service.MountCgroups
	if service.Privileged {
		if cgroupsMount == "" {
			cgroupsMount = "rw"
		}
		if seccomp == "" {
			seccomp = "unconfined"
		}
		spec.AddBindMount("/dev/net", "/dev/net", []string{"bind"})
	}

	// Mount cgroups
	if cgroupsMount != "" {
		if err = spec.AddCgroupsMount(cgroupsMount); err != nil {
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
		if sp.Annotations != nil {
			exposedPortsAnn = sp.Annotations["org.opencontainers.image.exposedPorts"]
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
	if seccomp == "" || seccomp == "default" {
		// Derive seccomp configuration (must be called as last)
		spec.SetLinuxSeccompDefault()
	} else if seccomp == "unconfined" {
		// Do not restrict operations with seccomp
		spec.SetLinuxSeccompUnconfined()
	} else {
		// Use seccomp configuration from file
		var j []byte
		if j, err = ioutil.ReadFile(res.ResolveFile(seccomp)); err != nil {
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
	}

	// Use host network by removing 'network' namespace
	if useHostNetwork {
		spec.UseHostNetwork()
	} else {
		spec.AddOrReplaceLinuxNamespace(specs.NetworkNamespace, "")
	}

	// Add hostname
	if service.Hostname != "" {
		spec.SetHostname(service.Hostname)
	}

	// Add network hook
	if len(networks) > 0 {
		spec.AddBindMountConfig("/etc/hostname")
		spec.AddBindMountConfig("/etc/hosts")
		spec.AddBindMountConfig("/etc/resolv.conf")
		hook, err := builder.NewHookBuilderFromSpec(sp)
		if err != nil {
			return err
		}
		hook.SetIPAMDataDir(ipamDataDir)
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
			hook.AddPortMapEntry(builder.PortMapEntry{
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
		if prootPath == "" {
			return errors.New("transform: port mapping only supported with contained container network. hint: add contained network, remove port mapping or, when rootless, enable proot")
		} else {
			for _, port := range service.Ports {
				if port.IP != "" {
					return errors.New("IP is not supported in proot port mappings")
				}
				spec.AddPRootPortMapping(strconv.Itoa(int(port.Published)), strconv.Itoa(int(port.Target)))
			}
		}
	}
	// TODO: support healthcheck (as Hook)
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

func ToSpecProcess(p *model.Process, prootPath string, builder *builder.SpecBuilder) (err error) {
	// Entrypoint & command
	if p.Entrypoint != nil {
		builder.SetProcessEntrypoint(p.Entrypoint)
		builder.SetProcessCmd([]string{})
	}
	if p.Command != nil {
		builder.SetProcessCmd(p.Command)
	}
	// Add proot
	if p.PRoot {
		if prootPath == "" {
			return errors.New("proot enabled but no proot path configured")
		}
		builder.SetPRootPath(prootPath)
	}

	// Env
	for k, v := range p.Environment {
		builder.AddProcessEnv(k, v)
	}

	// Working dir
	if p.Cwd != "" {
		builder.SetProcessCwd(p.Cwd)
	}

	// Terminal
	builder.SetProcessTerminal(p.Tty)

	// User
	if p.User != nil {
		// TODO: map additional groups
		builder.SetProcessUser(idutils.User{p.User.User, p.User.Group})
	}

	// Privileged
	capAdd := p.CapAdd
	if p.Privileged {
		capAdd = []string{"ALL"}
	}

	// Capabilities
	for _, addCap := range capAdd {
		if strings.ToUpper(addCap) == "ALL" {
			builder.AddAllProcessCapabilities()
			break
		} else if err = builder.AddProcessCapability("CAP_" + addCap); err != nil {
			return
		}
	}
	for _, dropCap := range p.CapDrop {
		if err = builder.DropProcessCapability("CAP_" + dropCap); err != nil {
			return
		}
	}

	builder.SetProcessApparmorProfile(p.ApparmorProfile)
	builder.SetProcessNoNewPrivileges(p.NoNewPrivileges)
	builder.SetProcessSelinuxLabel(p.SelinuxLabel)
	if p.OOMScoreAdj != nil {
		builder.SetProcessOOMScoreAdj(*p.OOMScoreAdj)
	}

	return nil
}

func toMounts(mounts []model.VolumeMount, res model.ResourceResolver, spec *builder.BundleBuilder) error {
	for _, m := range mounts {
		src, err := res.ResolveMountSource(m)
		if err != nil {
			return err
		}

		t := m.Type
		if t == "" || t == model.MOUNT_TYPE_VOLUME {
			t = model.MOUNT_TYPE_BIND
		}
		opts := m.Options
		if len(opts) == 0 {
			// Apply default mount options. See man7.org/linux/man-pages/man8/mount.8.html
			opts = []string{"bind", "nodev", "mode=0755"}
		} else {
			sliceutils.AddToSet(&opts, "bind")
		}

		sp := spec.Generator.Spec()
		sp.Mounts = append(sp.Mounts, specs.Mount{
			Type:        string(t),
			Source:      src,
			Destination: m.Target,
			Options:     opts,
		})
	}
	return nil
}
