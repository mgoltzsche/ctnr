package runcrunner

import (
	"bytes"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/mgoltzsche/ctnr/pkg/log"
	"github.com/mgoltzsche/ctnr/run"
	"github.com/pkg/errors"
)

var _ run.ContainerManager = &ContainerManager{}

type ContainerManager struct {
	runners map[string]run.Container
	rootDir string
	debug   log.FieldLogger
}

func NewContainerManager(rootDir string, loggers log.Loggers) (*ContainerManager, error) {
	absRoot, err := filepath.Abs(rootDir)
	if err != nil {
		return nil, err
	}
	return &ContainerManager{map[string]run.Container{}, absRoot, loggers.Debug}, nil
}

func (m *ContainerManager) NewContainer(cfg *run.ContainerConfig) (run.Container, error) {
	return NewRuncContainer(cfg, m.rootDir, m.debug)
}

func (m *ContainerManager) Get(id string) (run.Container, error) {
	panic("TODO: runcmanager.Get(id)")
	return nil, nil
}

func (m *ContainerManager) Kill(id string, signal os.Signal, all bool) error {
	var args []string
	if all {
		args = []string{"--root", m.rootDir, "kill", "--all=true", id, signal.String()}
	} else {
		args = []string{"--root", m.rootDir, "kill", id, signal.String()}
	}
	c := exec.Command("runc", args...)
	var buf bytes.Buffer
	c.Stdout = &buf
	c.Stderr = &buf
	if err := c.Run(); err != nil {
		return errors.Errorf("kill: %s: %s", err, strings.TrimRight(buf.String(), "\n"))
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
