package model

import (
	"fmt"

	"github.com/opencontainers/runc/libcontainer/specconv"
	"github.com/opencontainers/runtime-tools/generate"
	//"github.com/opencontainers/image-tools/image"
	"io/ioutil"
	"os"

	imgspecs "github.com/opencontainers/image-spec/specs-go/v1"
	specs "github.com/opencontainers/runtime-spec/specs-go"
	//"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
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

func (service *Service) ToSpec(img *imgspecs.Image, vols VolumeResolver, rootless bool, spec *generate.Generator) error {
	if rootless {
		specconv.ToRootless(spec.Spec())
	}

	err := applyService(img, service, spec)
	if err != nil {
		return err
	}

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
		if spec.Spec().Mounts == nil {
			spec.Spec().Mounts = []specs.Mount{}
		}
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
func applyService(img *imgspecs.Image, service *Service, spec *generate.Generator) error {
	// Apply args
	imgCfg := img.Config
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
	spec.SetProcessArgs(append(entrypoint, cmd...))

	// Apply env
	for _, e := range imgCfg.Env {
		kv := strings.SplitN(e, "=", 2)
		k := kv[0]
		v := ""
		if len(kv) == 2 {
			v = kv[1]
		}
		spec.AddProcessEnv(k, v)
	}
	for k, v := range service.Environment {
		spec.AddProcessEnv(k, fmt.Sprintf("%q", v))
	}

	// Apply cwd
	if service.Cwd != "" {
		spec.SetProcessCwd(service.Cwd)
	} else if imgCfg.WorkingDir != "" {
		spec.SetProcessCwd(imgCfg.WorkingDir)
	}

	spec.SetProcessTerminal(service.Tty)

	// Apply annotations
	if imgCfg.Labels != nil {
		for k, v := range imgCfg.Labels {
			spec.AddAnnotation(k, v)
		}
	}
	// TODO: extract annotations also from image index and manifest
	if img.Author != "" {
		spec.AddAnnotation("org.opencontainers.image.author", img.Author)
	}
	if !time.Unix(0, 0).Equal(*img.Created) {
		spec.AddAnnotation("org.opencontainers.image.created", img.Created.String())
	}
	spec.AddAnnotation(ANNOTATION_BUNDLE_CREATED, time.Now().String())
	//spec.AddAnnotation(ANNOTATION_BUNDLE_ID, id)
	/* TODO: enable if supported:
	if img.StopSignal != "" {
		spec.AddAnnotation("org.opencontainers.image.stopSignal", img.Config.StopSignal)
	}*/
	if service.StopSignal != "" {
		spec.AddAnnotation("org.opencontainers.image.stopSignal", service.StopSignal)
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
		sort.Strings(exposecsv)
		spec.AddAnnotation("org.opencontainers.image.exposedPorts", strings.Join(exposecsv, ","))
	}

	// TODO: apply user (username must be parsed from rootfs/etc/passwd and mapped to uid/gid)

	// TODO: mount separate paths in /proc/self/fd to apply service.StdinOpen
	spec.SetRootReadonly(service.ReadOnly)

	// TODO: mount volumes

	// TODO: register healthcheck (as Hook)
	// TODO: bind ports (propably in networking Hook)

	return nil
}
