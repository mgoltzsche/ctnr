package net

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/containernetworking/cni/libcni"
	//"github.com/vishvananda/netns"
	"golang.org/x/sys/unix"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func CreateNetNS(file string) error {
	// TODO: clean this up
	if strings.Index(file, "/var/run/netns/") != 0 {
		return fmt.Errorf("Only named network namespaces in /var/run/netns/ are supported")
	}
	name := file[15:]
	return runCmd("ip", "netns", "add", name)
	/*runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	ns, err := netns.New()
	if err != nil {
		return err
	}
	defer ns.Close()
	netnsFile, err := os.Readlink(fmt.Sprint("/proc/self/fd/", int(ns)))
	if err != nil {
		return fmt.Errorf("Cannot resolve file descriptor to network namespace: %v", err)
	}
	// DOESN'T WORK: link net:[4026532631] /var/run/netns/myservice: no such file or directory
	if err = os.Symlink(netnsFile, file); err != nil {
		return err
	}
	return nil*/
}

func DelNetNS(file string) error {
	// TODO: clean this up
	if strings.Index(file, "/var/run/netns/") != 0 {
		return fmt.Errorf("Only named network namespaces in /var/run/netns/ are supported")
	}
	name := file[15:]
	return runCmd("ip", "netns", "delete", name)
	//return os.Remove(file)
}

// Resolves the configured network by name
// and adds it to the process' current network namespace.
func AddNet(netNames []string) error {
	err := runCmd("/bin/ip", "netns", "list")
	fmt.Printf("## %v\n", err)
	//ns, err := currentNetns()
	//fmt.Println("## " + ns)

	cni := newCNI()
	rt, err := runtimeConf()
	if err != nil {
		return err
	}
	for _, netName := range netNames {
		netconf, err := networkConf(netName)
		if err != nil {
			return err
		}
		result, err := cni.AddNetworkList(netconf, rt)
		if err != nil {
			return err
		}
		result.Print()
		fmt.Println()
		// TODO: populate IP contained in result
	}
	return err
}

func DelNet(netNames []string) (err error) {
	cni := newCNI()
	rt, err := runtimeConf()
	if err != nil {
		return err
	}
	for _, netName := range netNames {
		netconf, err := networkConf(netName)
		if err != nil {
			return err
		}
		e := cni.DelNetworkList(netconf, rt)
		if e != nil {
			err = e
		}
	}
	return
}

func newCNI() libcni.CNI {
	cni := &libcni.CNIConfig{
		Path: filepath.SplitList(os.Getenv("CNI_PATH")),
	}
	if len(cni.Path) == 0 {
		cni.Path = []string{"/var/lib/cni"}
	}
	return cni
}

func networkConf(netName string) (*libcni.NetworkConfigList, error) {
	dir := os.Getenv("NETCONFPATH")
	if dir == "" {
		dir = "/etc/cni/net.d"
	}
	netconf, err := libcni.LoadConfList(dir, netName)
	if err != nil {
		return nil, fmt.Errorf("Could not load CNI network configurations: %v", err)
	}
	return netconf, nil
}

func runtimeConf() (*libcni.RuntimeConf, error) {
	netns, err := currentNetns()
	if err != nil {
		return nil, err
	}
	var cniArgs [][2]string
	args := os.Getenv("CNI_ARGS")
	if len(args) > 0 {
		cniArgs, err = parseArgs(args)
		if err != nil {
			return nil, err
		}
	}
	var capabilityArgs map[string]interface{}
	capabilityArgsValue := os.Getenv("CAP_ARGS")
	if len(capabilityArgsValue) > 0 {
		if err = json.Unmarshal([]byte(capabilityArgsValue), &capabilityArgs); err != nil {
			return nil, fmt.Errorf("Cannot read CAP_ARGS: ", err)
		}
	}
	return &libcni.RuntimeConf{
		ContainerID:    "cni",
		NetNS:          netns,
		IfName:         "eth0",
		Args:           cniArgs,
		CapabilityArgs: capabilityArgs,
	}, nil
}

/*func currentNetns() (string, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// Save the current network namespace
	ns, err := netns.Get()
	if err != nil {
		return "", err
	}
	defer ns.Close()
	return ns.UniqueId(), nil
}*/

func currentNetns() (netnsPath string, err error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	netnsPath = fmt.Sprintf("/proc/%d/task/%d/ns/net", os.Getpid(), unix.Gettid())
	_, err = os.Readlink(netnsPath)
	if err != nil {
		return netnsPath, fmt.Errorf("Cannot get current network namespace %q: %s", netnsPath, err)
	}
	return netnsPath, nil
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

func parseArgs(args string) ([][2]string, error) {
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
