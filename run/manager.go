package run

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/mgoltzsche/cntnr/log"
)

type ContainerInfo struct {
	ID     string
	Status string
}

type ContainerManager struct {
	runners map[string]Container
	rootDir string
	debug   log.Logger
}

func NewContainerManager(rootDir string, debug log.Logger) (*ContainerManager, error) {
	absRoot, err := filepath.Abs(rootDir)
	if err != nil {
		return nil, err
	}
	return &ContainerManager{map[string]Container{}, absRoot, debug}, nil
}

func (m *ContainerManager) Close() (err error) {
	return m.Stop()
}

func (m *ContainerManager) NewContainer(id string, bundle ContainerBundle) *RuncContainer {
	return NewRuncContainer(id, bundle, m.rootDir, m.debug)
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

func (m *ContainerManager) Deploy(c Container) error {
	err := c.Start()
	if err == nil {
		m.runners[c.ID()] = c
	}
	return err
}

func (m *ContainerManager) Stop() (err error) {
	for _, c := range m.runners {
		c.Stop()
	}
	for _, c := range m.runners {
		if e := c.Wait(); e != nil {
			if err == nil {
				err = e
			} else {
				err = multiError(e, err)
			}
		}
	}
	return err
}

func (m *ContainerManager) Wait() (err error) {
	for _, c := range m.runners {
		e := c.Wait()
		if e != nil {
			m.debug.Println(e)
			if err == nil {
				err = e
			}
		}
	}
	return err
}

func (m *ContainerManager) List() (r []ContainerInfo, err error) {
	r = []ContainerInfo{}
	if _, e := os.Stat(m.rootDir); !os.IsNotExist(e) {
		files, err := ioutil.ReadDir(m.rootDir)
		if err == nil {
			for _, f := range files {
				if _, e := os.Stat(filepath.Join(m.rootDir, f.Name(), "state.json")); !os.IsNotExist(e) {
					r = append(r, ContainerInfo{f.Name(), "running"})
				}
			}
		}
	}
	return
}

func (m *ContainerManager) HandleSignals() {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM, syscall.SIGKILL)
	go func() {
		<-sigs
		err := m.Stop()
		if err != nil {
			os.Stderr.WriteString(fmt.Sprintf("Failed to stop: %s\n", err))
		}
	}()
}
