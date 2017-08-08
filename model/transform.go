package model

import (
	"encoding/json"
	"fmt"
	//"github.com/mgoltzsche/cntnr/log"
	"github.com/mgoltzsche/cntnr/images"
	"github.com/opencontainers/runc/libcontainer/specconv"
	//"github.com/mgoltzsche/cntnr/libcontainer/specconv"
	//"github.com/opencontainers/image-tools/image"
	"github.com/mgoltzsche/cntnr/log"
	specs "github.com/opencontainers/runtime-spec/specs-go"
	"io/ioutil"
	"os"
	//"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type RuntimeBundleBuilder struct {
	Dir   string
	Image *images.Image
	Spec  *specs.Spec
}

func (b *RuntimeBundleBuilder) Build(debug log.Logger) error {
	//err = image.UnpackLayout(img.Directory, containerDir, "latest")
	//err = image.CreateRuntimeBundleLayout(img.Directory, containerDir, "latest", "rootfs")
	err := b.Image.Unpack(filepath.Join(b.Dir, b.Spec.Root.Path), debug)
	if err != nil {
		return fmt.Errorf("Unpacking OCI layout of image %q (%s) failed: %v", b.Image.Name, b.Image.Directory, err)
	}
	j, err := json.MarshalIndent(b.Spec, "", "  ")
	if err != nil {
		return fmt.Errorf("Cannot unmarshal OCI runtime spec for %s: %v", b.Dir, err)
	}
	err = ioutil.WriteFile(filepath.Join(b.Dir, "config.json"), j, 0770)
	if err != nil {
		return fmt.Errorf("Cannot write OCI runtime spec: %v", err)
	}
	return nil
}

func (service *Service) NewRuntimeBundleBuilder(containerID, bundleDir string, imgs *images.Images, vols VolumeResolver, rootless bool) (*RuntimeBundleBuilder, error) {
	if service.Name == "" {
		return nil, fmt.Errorf("Service has no name")
	}
	if service.Image == "" {
		return nil, fmt.Errorf("Service %q has no image", service.Name)
	}
	img, err := imgs.Image(service.Image)
	if err != nil {
		return nil, err
	}
	spec, err := service.toSpec(containerID, img, vols, rootless)
	if err != nil {
		return nil, err
	}
	return &RuntimeBundleBuilder{bundleDir, img, spec}, nil
}

func (service *Service) toSpec(containerID string, img *images.Image, vols VolumeResolver, rootless bool) (*specs.Spec, error) {
	spec := specconv.Example()

	err := applyService(img, service, spec)
	if err != nil {
		return nil, err
	}

	if rootless {
		specconv.ToRootless(spec)
	} else {
		// Add Linux capabilities
		cap := []string{
			"CAP_KILL",
			"CAP_CHOWN",
			"CAP_FSETID",
			"CAP_SETGID",
			"CAP_SETUID",
			"CAP_NET_BIND_SERVICE",
			"CAP_NET_RAW",
		}
		c := spec.Process.Capabilities
		addCap(&c.Bounding, cap)
		addCap(&c.Effective, cap)
		addCap(&c.Inheritable, cap)
		addCap(&c.Permitted, cap)
		addCap(&c.Ambient, cap)
	}

	// Add mounts
	for _, m := range service.Volumes {
		mount, err := vols.PrepareVolumeMount(m)
		if err != nil {
			return nil, err
		}
		spec.Mounts = append(spec.Mounts, mount)
	}

	if !rootless {
		// Limit resources
		if spec.Linux == nil {
			spec.Linux = &specs.Linux{}
		}
		if spec.Linux.Resources == nil {
			spec.Linux.Resources = &specs.LinuxResources{}
		}
		spec.Linux.Resources.Pids = &specs.LinuxPids{32771}
		spec.Linux.Resources.HugepageLimits = []specs.LinuxHugepageLimit{
			{
				Pagesize: "2MB",
				Limit:    9223372036854772000,
			},
		}
		// TODO: limit memory, cpu and blockIO access

		// Add network priority
		/*if spec.Linux.Resources == nil {
			spec.Linux.Resources = &specs.LinuxResources{}
		}
		if spec.Linux.Resources.Network == nil {
			spec.Linux.Resources.Network = &specs.LinuxNetwork{}
		}
		spec.Linux.Resources.Network.ClassID = ""
		spec.Linux.Resources.Network.Priorities = []specs.LinuxInterfacePriority{
			{"eth0", 2},
			{"lo", 1},
		}*/

		// Use host networks by removing 'network' namespace
		nss := spec.Linux.Namespaces
		for i, ns := range nss {
			if ns.Type == specs.NetworkNamespace {
				spec.Linux.Namespaces = append(nss[0:i], nss[i+1:]...)
				break
			}
		}

		// Add hostname
		hostname := service.Hostname
		domainname := service.Domainname
		if hostname == "" {
			hostname = containerID
		} else {
			dotPos := strings.Index(hostname, ".")
			if dotPos != -1 {
				domainname = hostname[dotPos+1:]
				hostname = hostname[:dotPos]
			}
		}
		fqn := strings.Trim(hostname+"."+domainname, ".")
		spec.Hostname = hostname

		// Add network hooks
		networks := []string{"default", "test"}
		if len(networks) > 0 {
			//hookBinary, err := exec.LookPath("cntnr-hooks")
			executable, err := os.Executable()
			if err != nil {
				return nil, fmt.Errorf("Cannot find network hook binary! %v", err)
			}
			cniPluginPaths := os.Getenv("CNI_PATH")
			if cniPluginPaths == "" {
				pluginPath := filepath.Join(filepath.Dir(executable), "..", "cni-plugins")
				if s, err := os.Stat(pluginPath); err == nil && s.IsDir() {
					cniPluginPaths = pluginPath
				}
			}
			if cniPluginPaths == "" {
				return nil, fmt.Errorf("CNI_PATH environment variable empty. It must contain paths to CNI plugins. See https://github.com/containernetworking/cni/blob/master/SPEC.md")
			}
			// TODO: add more CNI env vars
			cniEnv := []string{
				"PATH=" + os.Getenv("PATH"),
				"CNI_PATH=" + cniPluginPaths,
			}
			spec.Linux.Namespaces = append(spec.Linux.Namespaces, specs.LinuxNamespace{Type: specs.NetworkNamespace})
			spec.Hooks = &specs.Hooks{
				Prestart: []specs.Hook{},
				Poststop: []specs.Hook{},
			}

			hookArgs := []string{"cntnr", "net", "init", "-hostname=" + fqn}
			for _, dnsip := range service.Dns {
				hookArgs = append(hookArgs, "--dns="+dnsip)
			}
			for _, search := range service.DnsSearch {
				hookArgs = append(hookArgs, "--dns-search="+search)
			}
			for _, e := range service.ExtraHosts {
				hookArgs = append(hookArgs, "--hosts-entry="+e.Name+"="+e.Ip)
			}
			addHook(&spec.Hooks.Prestart, specs.Hook{
				Path: executable,
				Args: append(hookArgs, networks...),
				Env:  cniEnv,
			})
			addHook(&spec.Hooks.Poststop, specs.Hook{
				Path: executable,
				Args: append([]string{"cntnr", "net", "del"}, networks...),
				Env:  cniEnv,
			})
		}
	}

	return spec, nil
}

func addHook(h *[]specs.Hook, a specs.Hook) {
	*h = append(*h, a)
}

func addCap(c *[]string, add []string) {
	m := map[string]bool{}
	for _, e := range *c {
		m[e] = true
	}
	for _, e := range add {
		if _, ok := m[e]; !ok {
			*c = append(*c, e)
		}
	}
}

// See image to runtime spec conversion rules: https://github.com/opencontainers/image-spec/blob/master/conversion.md
func applyService(img *images.Image, service *Service, spec *specs.Spec) error {
	// Apply args
	imgCfg := img.Config.Config
	entrypoint := imgCfg.Entrypoint
	cmd := imgCfg.Cmd
	if entrypoint == nil {
		entrypoint = []string{}
	}
	if cmd == nil {
		cmd = []string{}
	}
	if service.Entrypoint != nil {
		entrypoint = service.Entrypoint
		cmd = []string{}
	}
	if service.Command != nil {
		cmd = service.Command
	}
	spec.Process.Args = append(entrypoint, cmd...)

	// Apply env
	env := map[string]string{}
	for _, e := range imgCfg.Env {
		kv := strings.SplitN(e, "=", 2)
		k := kv[0]
		v := ""
		if len(kv) == 2 {
			v = kv[1]
		}
		env[k] = v
	}
	for k, v := range service.Environment {
		env[k] = fmt.Sprintf("%q", v)
	}
	spec.Process.Env = make([]string, len(env))
	i := 0
	for k, v := range env {
		spec.Process.Env[i] = k + "=" + v
		i++
	}

	// Apply cwd
	spec.Process.Cwd = imgCfg.WorkingDir
	if service.Cwd != "" {
		spec.Process.Cwd = service.Cwd
	}
	if spec.Process.Cwd == "" {
		spec.Process.Cwd = "/"
	}

	spec.Process.Terminal = service.Tty

	// Apply annotations
	spec.Annotations = map[string]string{}
	// TODO: extract annotations from image index and manifest
	if img.Config.Author != "" {
		spec.Annotations["org.opencontainers.image.author"] = img.Config.Author
	}
	if !time.Unix(0, 0).Equal(*img.Config.Created) {
		spec.Annotations["org.opencontainers.image.created"] = img.Config.Created.String()
	}
	/* TODO: enable if supported:
	if img.StopSignal != "" {
		spec.Annotations["org.opencontainers.image.stopSignal"] = img.Config.StopSignal
	}*/
	if imgCfg.Labels != nil {
		for k, v := range imgCfg.Labels {
			spec.Annotations[k] = v
		}
	}
	if service.StopSignal != "" {
		spec.Annotations["org.opencontainers.image.stopSignal"] = service.StopSignal
	}

	// Add exposed ports
	expose := map[string]bool{}
	if imgCfg.ExposedPorts != nil {
		for k := range imgCfg.ExposedPorts {
			expose[k] = true
		}
	}
	if service.Expose != nil {
		for _, e := range service.Expose {
			expose[e] = true
		}
	}
	if len(expose) > 0 {
		exposecsv := make([]string, len(expose))
		i := 0
		for k := range expose {
			exposecsv[i] = k
			i++
		}
		spec.Annotations["org.opencontainers.image.exposedPorts"] = strings.Join(exposecsv, ",")
	}

	// TODO: apply user (username must be parsed from rootfs/etc/passwd and mapped to uid/gid)

	// TODO: mount separate paths in /proc/self/fd to apply service.StdinOpen
	spec.Root.Readonly = service.ReadOnly

	// TODO: mount volumes

	// TODO: register healthcheck (as Hook)
	// TODO: bind ports (propably in networking Hook)

	return nil
}
