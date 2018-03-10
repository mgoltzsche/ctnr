package librunner

import (
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/hashicorp/go-multierror"
	"github.com/mgoltzsche/cntnr/pkg/log"
	"github.com/mgoltzsche/cntnr/run"
	"github.com/opencontainers/runc/libcontainer"
	"github.com/pkg/errors"
)

var _ run.ContainerManager = &ContainerManager{}

type ContainerManager struct {
	factory  libcontainer.Factory
	runners  map[string]run.Container
	rootDir  string
	rootless bool
	loggers  log.Loggers
}

func NewContainerManager(rootDir string, rootless bool, loggers log.Loggers) (r *ContainerManager, err error) {
	absRoot, err := filepath.Abs(rootDir)
	if err != nil {
		return
	}
	r = &ContainerManager{runners: map[string]run.Container{}, rootDir: absRoot, rootless: rootless, loggers: loggers}
	binary, err := os.Executable()
	if err != nil {
		return nil, errors.Wrapf(err, "new container manager: resolve %q executable", os.Args[0])
	}
	if r.factory, err = libcontainer.New(rootDir, libcontainer.Cgroupfs, libcontainer.InitArgs(binary, "init")); err != nil {
		return
	}
	return
}

func (m *ContainerManager) NewContainer(cfg *run.ContainerConfig) (c run.Container, err error) {
	c, err = NewContainer(cfg, m.rootless, m.factory, m.loggers)
	return c, errors.Wrap(err, "new container")
}

func (m *ContainerManager) Get(id string) (run.Container, error) {
	return LoadContainer(id, m.factory, m.loggers)
}

func (m *ContainerManager) Kill(id, signal string, all bool) error {
	panic("TODO: kill any container that might not be controlled by this process")
}

func (m *ContainerManager) List() (r []run.ContainerInfo, err error) {
	r = []run.ContainerInfo{}
	if _, e := os.Stat(m.rootDir); !os.IsNotExist(e) {
		files, e := ioutil.ReadDir(m.rootDir)
		if e == nil {
			for _, f := range files {
				if _, e = os.Stat(filepath.Join(m.rootDir, f.Name(), "state.json")); !os.IsNotExist(e) {
					r = append(r, run.ContainerInfo{f.Name(), "running"})
				}
			}
		} else {
			err = multierror.Append(err, e)
		}
	}
	return
}
