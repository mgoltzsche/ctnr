package model

import (
	"fmt"
	"io/ioutil"
	"os"

	"github.com/mgoltzsche/cntnr/generate"
	//"github.com/mgoltzsche/cntnr/pkg/atomic"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mgoltzsche/cntnr/pkg/sliceutils"
	specs "github.com/opencontainers/runtime-spec/specs-go"
)

const (
	ANNOTATION_BUNDLE_IMAGE_NAME = "com.github.mgoltzsche.cntnr.bundle.image.name"
	ANNOTATION_BUNDLE_CREATED    = "com.github.mgoltzsche.cntnr.bundle.created"
	ANNOTATION_BUNDLE_ID         = "com.github.mgoltzsche.cntnr.bundle.id"
)

func (service *Service) ToSpec(p *Project, rootless bool, spec *generate.SpecBuilder) (err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("generate OCI bundle spec: %s", err)
		}
	}()

	vols := NewVolumeResolver(p)

	if rootless {
		spec.ToRootless()
	}

	if err := applyService(service, vols, spec); err != nil {
		return err
	}

	if !rootless {
		// Limit resources
		spec.SetLinuxResourcesPidsLimit(32771)
		spec.AddLinuxResourcesHugepageLimit("2MB", 9223372036854772000)
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
		return fmt.Errorf("transform: multiple networks are not supported when 'host' or 'none' network supplied")
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
		return fmt.Errorf("transform: no networks supported in rootless mode")
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
		return fmt.Errorf("transform: port mapping only supported with container network - add network or remove port mapping")
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

// See image to runtime spec conversion rules: https://github.com/opencontainers/image-spec/blob/master/conversion.md
func applyService(service *Service, vols VolumeResolver, spec *generate.SpecBuilder) error {
	// Entrypoint & command
	if service.Entrypoint != nil {
		spec.SetProcessEntrypoint(service.Entrypoint)
		spec.SetProcessCmd([]string{})
	}
	if service.Command != nil {
		spec.SetProcessCmd(service.Command)
	}

	// Env
	for k, v := range service.Environment {
		spec.AddProcessEnv(k, v)
	}

	// Working dir
	if service.Cwd != "" {
		spec.SetProcessCwd(service.Cwd)
	}

	// Terminal
	spec.SetProcessTerminal(service.Tty)

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

	// TODO: apply user (username must be parsed from rootfs/etc/passwd and mapped to uid/gid)

	// TODO: mount separate paths in /proc/self/fd to apply service.StdinOpen
	spec.SetRootReadonly(service.ReadOnly)

	// Add mounts
	for _, m := range service.Volumes {
		mount, err := vols.PrepareVolumeMount(m)
		if err != nil {
			return err
		}
		spec.Spec().Mounts = append(spec.Spec().Mounts, mount)
	}

	return nil
}
