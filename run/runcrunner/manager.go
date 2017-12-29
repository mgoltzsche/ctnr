package runcrunner

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/mgoltzsche/cntnr/log"
	"github.com/mgoltzsche/cntnr/run"
)

type ContainerManager struct {
	runners map[string]run.Container
	rootDir string
	debug   log.Logger
}

func NewContainerManager(rootDir string, debug log.Logger) (*ContainerManager, error) {
	absRoot, err := filepath.Abs(rootDir)
	if err != nil {
		return nil, err
	}
	return &ContainerManager{map[string]run.Container{}, absRoot, debug}, nil
}

func (m *ContainerManager) NewContainer(id string, bundle run.ContainerBundle, ioe run.ContainerIO) (run.Container, error) {
	return NewRuncContainer(id, bundle, m.rootDir, ioe, m.debug), nil
}

func (m *ContainerManager) Kill(id, signal string, all bool) error {
	var args []string
	if all {
		args = []string{"--root", m.rootDir, "kill", "--all=true", id, signal}
	} else {
		args = []string{"--root", m.rootDir, "kill", id, signal}
	}
	c := exec.Command("runc", args...)
	var buf bytes.Buffer
	c.Stdout = &buf
	c.Stderr = &buf
	if err := c.Run(); err != nil {
		return fmt.Errorf("%s: %s", err, strings.TrimRight(buf.String(), "\n"))
	}
	return nil
}

func (m *ContainerManager) List() (r []run.ContainerInfo, err error) {
	r = []run.ContainerInfo{}
	if _, e := os.Stat(m.rootDir); !os.IsNotExist(e) {
		files, err := ioutil.ReadDir(m.rootDir)
		if err == nil {
			for _, f := range files {
				if _, e := os.Stat(filepath.Join(m.rootDir, f.Name(), "state.json")); !os.IsNotExist(e) {
					r = append(r, run.ContainerInfo{f.Name(), "running"})
				}
			}
		}
	}
	return
}