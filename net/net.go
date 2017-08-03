package net

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/containernetworking/cni/libcni"
	"github.com/containernetworking/cni/pkg/types"
	"github.com/containernetworking/cni/pkg/types/current"
	"github.com/vishvananda/netns"
	//"golang.org/x/sys/unix"
	specs "github.com/opencontainers/runtime-spec/specs-go"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

type ipConfig struct {
	interfaceName string
	version       string
	address       net.IPNet
}

type ContainerNetManager struct {
	state          *specs.State
	rootfs         string
	hostname       string
	cni            *libcni.CNIConfig
	netNS          string
	confDir        string
	confFiles      []string
	configs        map[string]*libcni.NetworkConfig
	cniArgs        [][2]string
	capabilityArgs map[string]interface{}
	dns            DNS
	hosts          map[string]string
	hostsOrder     []string
	//ips            []*ipConfig
}

func NewContainerNetManager(s *specs.State) (*ContainerNetManager, error) {
	cni := &libcni.CNIConfig{
		Path: filepath.SplitList(os.Getenv("CNI_PATH")),
	}
	if len(cni.Path) == 0 {
		cni.Path = []string{"/var/lib/cni"}
	}

	// Init network config resolution
	netConfDir := os.Getenv("NETCONFPATH")
	if netConfDir == "" {
		netConfDir = "/etc/cni/net.d"
	}
	confFiles, err := libcni.ConfFiles(netConfDir, []string{".conf", ".json"})
	if err != nil {
		return nil, fmt.Errorf("Could not find CNI network configuration files: %s", err)
	}
	sort.Strings(confFiles)

	// Get container's network namespace
	netns, err := getProcessNetns(s.Pid)
	if err != nil {
		return nil, err
	}

	// Parse CNI_ARGS
	var cniArgs [][2]string
	args := os.Getenv("CNI_ARGS")
	if len(args) > 0 {
		cniArgs, err = parseCniArgs(args)
		if err != nil {
			return nil, err
		}
	}

	// Parse CAP_ARGS
	var capabilityArgs map[string]interface{}
	capabilityArgsValue := os.Getenv("CAP_ARGS")
	if len(capabilityArgsValue) > 0 {
		if err = json.Unmarshal([]byte(capabilityArgsValue), &capabilityArgs); err != nil {
			return nil, fmt.Errorf("Cannot read CAP_ARGS: ", err)
		}
	}

	// Get rootfs and load bundle
	if s.Bundle == "" {
		return nil, fmt.Errorf("No bundle specified in container state")
	}
	spec, err := loadBundleSpec(s.Bundle)
	if err != nil {
		return nil, err
	}
	rootfs := filepath.Join(s.Bundle, spec.Root.Path)
	hostname := spec.Hostname
	if hostname == "" {
		hostname = s.ID
	}

	return &ContainerNetManager{
		state:          s,
		rootfs:         rootfs,
		hostname:       hostname,
		cni:            cni,
		netNS:          netns,
		confDir:        netConfDir,
		confFiles:      confFiles,
		configs:        map[string]*libcni.NetworkConfig{},
		cniArgs:        cniArgs,
		capabilityArgs: capabilityArgs,
		dns:            DNS{[]string{}, []string{}, []string{}, ""},
		hosts:          map[string]string{},
		hostsOrder:     []string{},
		//ips:            []*ipConfig{},
	}, nil
}

// Resolves the configured CNI network by name
// and adds it to the container process' network namespace.
func (m *ContainerNetManager) AddNet(ifName, netName string) (r types.Result, err error) {
	netConf, err := m.netConf(netName)
	if err == nil {
		r, err = m.cni.AddNetwork(netConf, m.rtConf(ifName))
		if err != nil {
			return
		}
		rs, err := current.NewResultFromResult(r)
		if err != nil {
			return nil, fmt.Errorf("Could not convert CNI result: %s", err)
		}
		r = rs
		m.dns.Nameserver = append(m.dns.Nameserver, rs.DNS.Nameservers...)
		m.dns.Search = append(m.dns.Search, rs.DNS.Search...)
		m.dns.Options = append(m.dns.Options, rs.DNS.Options...)
		if rs.DNS.Domain != "" && m.dns.Domain == "" {
			m.dns.Domain = rs.DNS.Domain
		}
		/*for _, ip := range rs.IPs {
			fmt.Println(ip.Interface, "##")
			itrfc := rs.Interfaces[*ip.Interface]
			m.ips = append(m.ips, &ipConfig{itrfc.Name, ip.Version, net.IPNet(ip.Address)})
		}*/
	}
	return
}

func (m *ContainerNetManager) DelNet(ifName, netName string) (err error) {
	netConf, err := m.netConf(netName)
	if err == nil {
		err = m.cni.DelNetwork(netConf, m.rtConf(ifName))
	}
	return
}

func (m *ContainerNetManager) SetHostname(hostname string) {
	m.hostname = hostname
}

func (m *ContainerNetManager) AddHostnameHostsEntry(ifName string) error {
	ip, err := m.getIP(ifName)
	if err != nil {
		return err
	}
	hostname := m.hostname
	dotPos := strings.Index(hostname, ".")
	if dotPos == -1 {
		m.AddHostsEntry(hostname, ip)
	} else {
		// Handle FQN
		m.AddHostsEntry(hostname, ip)
		m.AddHostsEntry(hostname[:dotPos], ip)
	}
	return nil
}

func (m *ContainerNetManager) AddHostsEntry(host, ip string) {
	m.hostsOrder = append(m.hostsOrder, host)
	m.hosts[host] = ip
}

func (m *ContainerNetManager) AddDNS(dns DNS) {
	if len(dns.Nameserver) > 0 {
		m.dns.Nameserver = append(m.dns.Nameserver, dns.Nameserver...)
	}
	if dns.Domain != "" {
		m.dns.Domain = dns.Domain
	}
	if len(dns.Search) > 0 {
		m.dns.Search = append(m.dns.Search, dns.Search...)
	}
	if len(dns.Options) > 0 {
		m.dns.Options = append(m.dns.Options, dns.Options...)
	}
}

func (m *ContainerNetManager) Apply() error {
	// Create /etc dir in bundle's rootfs
	etcDir := filepath.Join(m.rootfs, "etc")
	if _, err := os.Stat(etcDir); os.IsNotExist(err) {
		if err = os.Mkdir(etcDir, 0755); err != nil {
			return err
		}
	}
	// Write /etc/hostname
	hostname := m.hostname
	dotPos := strings.Index(hostname, ".")
	if dotPos != -1 {
		hostname = hostname[:dotPos]
	}
	if err := writeFile(filepath.Join(etcDir, "hostname"), hostname+"\n"); err != nil {
		return fmt.Errorf("Cannot write hostname file: %s", err)
	}
	// Write /etc/resolv.conf if value set
	if err := m.dns.write(filepath.Join(etcDir, "resolv.conf")); err != nil {
		return err
	}
	// Write /etc/hosts if not empty
	if len(m.hosts) > 0 {
		return writeHostsFile(filepath.Join(etcDir, "hosts"), m.hosts, m.hostsOrder)
	}
	return nil
}

func (m *ContainerNetManager) getIP(ifName string) (string, error) {
	// TODO: maybe use IP from CNI result

	/*for _, ip := range m.ips {
		if ip.interfaceName == ifName {
			return ip.address.IP.String(), nil
		}
	}
	return "", fmt.Errorf("Cannot find IP for interface %q", ifName)*/

	// Enter container's network namespace
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ns, err := netns.GetFromPid(m.state.Pid)
	if err != nil {
		return "", fmt.Errorf("Cannot find network namespace of process with PID %d: %s", m.state.Pid, err)
	}
	currns, err := netns.Get()
	if err != nil {
		return "", fmt.Errorf("Could not get current netns: %s", err)
	}
	err = netns.Set(ns)
	if err != nil {
		return "", fmt.Errorf("Could not enter container netns: %s", err)
	}
	defer netns.Set(currns)
	return getIP(ifName)
}

func getIP(ifName string) (string, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "", err
	}
	ifaceFound := false
	for _, i := range ifaces {
		fmt.Println(i.Name)
		if i.Name == ifName {
			ifaceFound = true
			addrs, err := i.Addrs()
			if err != nil {
				return "", err
			}
			for _, addr := range addrs {
				switch v := addr.(type) {
				case *net.IPNet:
					return v.IP.String(), nil
				case *net.IPAddr:
					return v.IP.String(), nil
				}
			}
		}
	}
	if ifaceFound {
		err = fmt.Errorf("Network interface %s has no IP", ifName)
	} else {
		err = fmt.Errorf("Network interface %q does not exist", ifName)
	}
	return "", err
}

func (m *ContainerNetManager) rtConf(ifName string) *libcni.RuntimeConf {
	return &libcni.RuntimeConf{
		ContainerID:    m.state.ID,
		NetNS:          m.netNS,
		IfName:         ifName,
		Args:           m.cniArgs,
		CapabilityArgs: m.capabilityArgs,
	}
}

func (m *ContainerNetManager) netConf(netName string) (*libcni.NetworkConfig, error) {
	c := m.configs[netName]
	if c == nil {
		for i, confFile := range m.confFiles {
			conf, err := libcni.ConfFromFile(confFile)
			if err != nil {
				return nil, err
			}
			if conf.Network.Name != "" {
				if m.configs[conf.Network.Name] == nil {
					// Duplicate network ignored as in original cnitool implementation
					m.configs[conf.Network.Name] = conf
				}
				if conf.Network.Name == netName {
					m.confFiles = m.confFiles[i+1:]
					return conf, nil
				}
			}
		}
		m.confFiles = []string{}
		return nil, fmt.Errorf("Network configuration %q not found in %q", netName, m.confDir)
	}
	return c, nil
}

func getProcessNetns(pid int) (netnsPath string, err error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	netnsPath = fmt.Sprintf("/proc/%d/ns/net", pid)
	_, err = os.Readlink(netnsPath)
	if err != nil {
		return netnsPath, fmt.Errorf("Cannot get network namespace %s: %s", netnsPath, err)
	}
	return netnsPath, nil
}

func parseCniArgs(args string) ([][2]string, error) {
	var result [][2]string

	pairs := strings.Split(args, ";")
	for _, pair := range pairs {
		kv := strings.Split(pair, "=")
		if len(kv) != 2 || kv[0] == "" || kv[1] == "" {
			return nil, fmt.Errorf("invalid CNI_ARGS pair %q", pair)
		}

		result = append(result, [2]string{kv[0], kv[1]})
	}

	return result, nil
}

func CreateNetNS(file string) error {
	// TODO: clean this up
	if strings.Index(file, "/var/run/netns/") != 0 {
		return fmt.Errorf("Only named network namespaces in /var/run/netns/ are supported")
	}
	name := file[15:]
	return runCmd("ip", "netns", "add", name)
	/* // anonymous network namespace
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	ns, err := netns.New()
	if err != nil {
		return err
	}
	defer ns.Close()
	return fmt.Sprint("/proc/self/fd/", int(ns)), nil */
}

func DelNetNS(file string) error {
	// TODO: clean this up
	if strings.Index(file, "/var/run/netns/") != 0 {
		return fmt.Errorf("Only named network namespaces in /var/run/netns/ are supported")
	}
	name := file[15:]
	return runCmd("ip", "netns", "delete", name)
}

func runCmd(c string, args ...string) error {
	cmd := exec.Command(c, args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("%s%s: %v", out.String(), strings.Join(append([]string{c}, args...), " "), err)
	}
	return nil
}
