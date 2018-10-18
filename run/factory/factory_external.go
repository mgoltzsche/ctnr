// +build !mgoltzsche_ctnr_libcontainer

package factory

import (
	"github.com/mgoltzsche/ctnr/pkg/log"
	"github.com/mgoltzsche/ctnr/run"
	"github.com/mgoltzsche/ctnr/run/runcrunner"
)

func NewContainerManager(rootDir string, rootless bool, loggers log.Loggers) (run.ContainerManager, error) {
	return runcrunner.NewContainerManager(rootDir, loggers)
}
