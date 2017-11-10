package model

import (
	"fmt"

	"github.com/mgoltzsche/cntnr/generate"
	"github.com/opencontainers/runc/libcontainer/specconv"
	//"github.com/opencontainers/image-tools/image"
	"io/ioutil"
	"os"

	specs "github.com/opencontainers/runtime-spec/specs-go"
	//"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

const (
	ANNOTATION_BUNDLE_IMAGE_NAME = "com.github.mgoltzsche.cntnr.bundle.image.name"
	ANNOTATION_BUNDLE_CREATED    = "com.github.mgoltzsche.cntnr.bundle.created"
	ANNOTATION_BUNDLE_ID         = "com.github.mgoltzsche.cntnr.bundle.id"
)

// TODO: put somewhere
/*// Copy host's /etc/hostname into bundle
if err = copyHostFile("/etc/hostname", rootDir); err != nil {
	return
}
// Copy host's /etc/hosts into bundle
if err = copyHostFile("/etc/hosts", rootDir); err != nil {
	return
}
// Copy host's /etc/resolv.conf into bundle if image didn't provide resolv.conf
resolvConf := filepath.Join(rootDir, "/etc/resolv.conf")
if _, e := os.Stat(resolvConf); os.IsNotExist(e) {
	if err = copyHostFile("/etc/resolv.conf", rootDir); err != nil {
		return
	}
}*/

/*func makeRootless(spec *generate.Generator) {
	// Remove network namespace
	spec.RemoveLinuxNamespace(specs.NetworkNamespace)
	// Add user namespace
	spec.AddOrReplaceLinuxNamespace(specs.UserNamespace, "")
	// Add user/group ID mapping
	spec.AddLinuxUIDMapping(uint32(os.Geteuid()), 0, 1)
	spec.AddLinuxGIDMapping(uint32(os.Getegid()), 0, 1)
	// Remove cgroup settings
	spec.Spec().Linux.Resources = nil

	// Fix mounts (taken from github.com/opencontainers/runc/libcontainer/specconv
	var mounts []specs.Mount
	for _, mount := range spec.Mounts {
		// Ignore all mounts that are under /sys.
		if strings.HasPrefix(mount.Destination, "/sys") {
			continue
		}

		// Remove all gid= and uid= mappings.
		var options []string
		for _, option := range mount.Options {
			if !strings.HasPrefix(option, "gid=") && !strings.HasPrefix(option, "uid=") {
				options = append(options, option)
			}
		}

		mount.Options = options
		mounts = append(mounts, mount)
	}
	// Add the sysfs mount as an rbind.
	mounts = append(mounts, specs.Mount{
		Source:      "/sys",
		Destination: "/sys",
		Type:        "none",
		Options:     []string{"rbind", "nosuid", "noexec", "nodev", "ro"},
	})
	spec.Mounts = mounts
}*/

func (service *Service) ToSpec(p *Project, rootless bool, spec *generate.SpecBuilder) error {
	vols := NewVolumeResolver(p)

	if rootless {
		specconv.ToRootless(spec.Spec())
	}

	applyService(service, spec)

	/*if rootless {
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
	}*/

	// Add mounts
	for _, m := range service.Volumes {
		mount, err := vols.PrepareVolumeMount(m)
		if err != nil {
			return err
		}
		spec.Spec().Mounts = append(spec.Spec().Mounts, mount)
	}

	if !rootless {
		// Limit resources
		spec.SetLinuxResourcesPidsLimit(32771)
		spec.AddLinuxResourcesHugepageLimit("2MB", 9223372036854772000)
		// TODO: limit memory, cpu and blockIO access

		/*// Add network priority
		spec.Linux.Resources.Network.ClassID = ""
		spec.Linux.Resources.Network.Priorities = []specs.LinuxInterfacePriority{
			{"eth0", 2},
			{"lo", 1},
		}*/
	}

	// TODO: read networks from compose file or CLI
	networks := []string{"default"}
	if rootless {
		networks = []string{}
	}
	useHostNetwork := len(networks) == 0

	// Use host networks by removing 'network' namespace
	if useHostNetwork {
		spec.RemoveLinuxNamespace(specs.NetworkNamespace)
	}

	// Add hostname
	hostname := service.Hostname
	domainname := service.Domainname
	if hostname != "" {
		dotPos := strings.Index(hostname, ".")
		if dotPos != -1 {
			domainname = hostname[dotPos+1:]
			hostname = hostname[:dotPos]
		}
	}
	spec.SetHostname(hostname)

	// Add network hooks
	if !useHostNetwork && len(networks) > 0 {
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
		if hostname != "" {
			hookArgs = append(hookArgs, "--hostname="+hostname)
		}
		if domainname != "" {
			hookArgs = append(hookArgs, "--domainname="+domainname)
		}
		for _, dnsip := range service.Dns {
			hookArgs = append(hookArgs, "--dns="+dnsip)
		}
		for _, search := range service.DnsSearch {
			hookArgs = append(hookArgs, "--dns-search="+search)
		}
		for _, opt := range service.DnsOptions {
			hookArgs = append(hookArgs, "--dns-opts="+opt)
		}
		for _, e := range service.ExtraHosts {
			hookArgs = append(hookArgs, "--hosts-entry="+e.Name+"="+e.Ip)
		}
		for _, p := range service.Ports {
			hookArgs = append(hookArgs, "--publish="+p.String())
		}
		spec.AddPreStartHook(executable, append(hookArgs, networks...))
		spec.AddPreStartHookEnv(executable, cniEnv)
		spec.AddPostStopHook(executable, append([]string{"cntnr", "net", "rm"}, networks...))
		spec.AddPostStopHookEnv(executable, cniEnv)
	}

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
func applyService(service *Service, spec *generate.SpecBuilder) {
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
		spec.AddProcessEnv(k, fmt.Sprintf("%q", v))
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

	// TODO: mount volumes

	// TODO: register healthcheck (as Hook)
	// TODO: bind ports (propably in networking Hook)
}
