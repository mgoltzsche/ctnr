package net

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/containernetworking/cni/libcni"
	"github.com/containernetworking/cni/pkg/types/current"
	//"github.com/vishvananda/netns"
	specs "github.com/opencontainers/runtime-spec/specs-go"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

type NetConfigs struct {
	confFiles []string
	configs   map[string]*libcni.NetworkConfig
}

func NewNetConfigs(confDir string) (*NetConfigs, error) {
	if confDir == "" {
		confDir = os.Getenv("NETCONFPATH")
		if confDir == "" {
			confDir = "/etc/cni/net.d"
		}
	}
	confFiles, err := libcni.ConfFiles(confDir, []string{".conf", ".json"})
	if err != nil {
		return nil, fmt.Errorf("Could not find CNI network configuration files: %s", err)
	}
	sort.Strings(confFiles)
	return &NetConfigs{confFiles, map[string]*libcni.NetworkConfig{}}, nil
}

func (n *NetConfigs) GetNet(name string) (*libcni.NetworkConfig, error) {
	c := n.configs[name]
	if c == nil {
		for i, confFile := range n.confFiles {
			conf, err := libcni.ConfFromFile(confFile)
			if err != nil {
				return nil, err
			}
			if conf.Network.Name != "" {
				if n.configs[conf.Network.Name] == nil {
					// Duplicate network ignored as in original cnitool implementation
					n.configs[conf.Network.Name] = conf
				}
				if conf.Network.Name == name {
					n.confFiles = n.confFiles[i+1:]
					return conf, nil
				}
			}
		}
		n.confFiles = []string{}
		return nil, fmt.Errorf("Network configuration %q not found", name)
	}
	return c, nil
}

type NetManager struct {
	id             string
	netNS          string
	cniArgs        [][2]string
	capabilityArgs map[string]interface{}
	cni            *libcni.CNIConfig
}

func NewNetManager(state *specs.State) (r *NetManager, err error) {
	netPaths := filepath.SplitList(os.Getenv("CNI_PATH"))
	if len(netPaths) == 0 {
		netPaths = []string{"/var/lib/cni"}
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

	var netns string
	if state.Pid > 0 {
		netns = fmt.Sprintf("/proc/%d/ns/net", state.Pid)
	}

	r = &NetManager{
		id:             state.ID,
		netNS:          netns,
		cniArgs:        cniArgs,
		capabilityArgs: capabilityArgs,
		cni:            &libcni.CNIConfig{Path: netPaths},
	}
	return
}

// Resolves the configured CNI network by name
// and adds it to the container process' network namespace.
func (m *NetManager) AddNet(ifName string, netConf *libcni.NetworkConfig) (r *current.Result, err error) {
	rs, err := m.cni.AddNetwork(netConf, m.rtConf(ifName))
	if err != nil {
		return
	}
	r, err = current.NewResultFromResult(rs)
	if err != nil {
		return nil, fmt.Errorf("Could not convert CNI result for network %s: %s", netConf.Network.Name, err)
	}
	return
}

func (m *NetManager) DelNet(ifName string, netConf *libcni.NetworkConfig) (err error) {
	return m.cni.DelNetwork(netConf, m.rtConf(ifName))
}

func (m *NetManager) rtConf(ifName string) *libcni.RuntimeConf {
	return &libcni.RuntimeConf{
		ContainerID:    m.id,
		NetNS:          m.netNS,
		IfName:         ifName,
		Args:           m.cniArgs,
		CapabilityArgs: m.capabilityArgs,
	}
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
