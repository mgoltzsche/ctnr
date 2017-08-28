package run

import (
	"fmt"
	"github.com/mgoltzsche/cntnr/log"
	"github.com/mgoltzsche/cntnr/model"
	specs "github.com/opencontainers/runtime-spec/specs-go"
	"io/ioutil"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
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

func (m *ContainerManager) NewContainer(id, bundleDir string, spec *specs.Spec, bindStdin bool) (Container, error) {
	/*c := exec.Command("runc", "--root", rootDir, "create", id)
	c.Dir = runtimeBundleDir
	c.Stderr = os.Stderr
	c.Stdout = os.Stdout
	err := c.Run()
	if err != nil {
		return nil, fmt.Errorf("Error: runc container creation: %s", err)
	}*/

	if id == "" {
		id = spec.Annotations[model.ANNOTATION_BUNDLE_ID]
		if id == "" {
			id = GenerateId()
		}
	}
	c := exec.Command("runc", "--root", m.rootDir, "run", id)
	c.Dir = bundleDir
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr

	if bindStdin || spec.Process.Terminal {
		c.Stdin = os.Stdin
	}

	if !spec.Process.Terminal {
		// Run in separate process group to be able to control orderly shutdown
		c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	}

	return &RuncContainer{id, c, m.debug}, nil
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
		e := c.Stop()
		if e != nil {
			m.debug.Printf("Failed to stop container %s: %v", c.ID(), err)
			if err == nil {
				err = e
			}
		}
	}
	return err
}

func (m *ContainerManager) Wait() (err error) {
	for _, c := range m.runners {
		e := c.Wait()
		if e != nil {
			m.debug.Printf("Failed to wait for container %s: %v", c.ID(), err)
			if err == nil {
				err = e
			}
		}
	}
	return err
}

func (m *ContainerManager) List() (r []ContainerInfo, err error) {
	r = []ContainerInfo{}
	files, err := ioutil.ReadDir(m.rootDir)
	if err == nil {
		for _, f := range files {
			if _, e := os.Stat(filepath.Join(m.rootDir, f.Name(), "state.json")); !os.IsNotExist(e) {
				r = append(r, ContainerInfo{f.Name(), "running"})
			}
		}
	}
	return r, err
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
