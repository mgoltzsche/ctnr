package net

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/containernetworking/cni/libcni"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func CreateNetNS(containerID, netName string) error {
	netdir := os.Getenv("NETCONFPATH")
	if netdir == "" {
		netdir = "/etc/cni/net.d"
	}
	netconf, err := libcni.LoadConfList(netdir, netName)
	if err != nil {
		return fmt.Errorf("Could not load CNI network: %v", err)
	}

	var capabilityArgs map[string]interface{}
	capabilityArgsValue := os.Getenv("CAP_ARGS")
	if len(capabilityArgsValue) > 0 {
		if err = json.Unmarshal([]byte(capabilityArgsValue), &capabilityArgs); err != nil {
			return fmt.Errorf("Cannot read CAP_ARGS: ", err)
		}
	}

	var cniArgs [][2]string
	args := os.Getenv("CNI_ARGS")
	if len(args) > 0 {
		cniArgs, err = parseArgs(args)
		if err != nil {
			return err
		}
	}

	err = run("ip", "netns", "add", containerID)
	if err != nil {
		return err
	}

	cninet := &libcni.CNIConfig{
		Path: filepath.SplitList(os.Getenv("CNI_PATH")),
	}

	if len(cninet.Path) == 0 {
		cninet.Path = []string{"/var/lib/cni"}
	}

	rt := &libcni.RuntimeConf{
		ContainerID:    "cni",
		NetNS:          "/var/run/netns/" + containerID,
		IfName:         "eth0",
		Args:           cniArgs,
		CapabilityArgs: capabilityArgs,
	}

	_, err = cninet.AddNetworkList(netconf, rt)
	return err
}

func DeleteNetNS(containerID string) error {
	return run("ip", "netns", "delete", containerID)
}

func run(c string, args ...string) error {
	cmd := exec.Command(c, args...)
	var out bytes.Buffer
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
