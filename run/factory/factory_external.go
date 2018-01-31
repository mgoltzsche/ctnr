// +build !mgoltzsche_cntnr_libcontainer

package factory

import (
	"github.com/mgoltzsche/cntnr/log"
	"github.com/mgoltzsche/cntnr/run"
	"github.com/mgoltzsche/cntnr/run/runcrunner"
)

func NewContainerManager(rootDir string, rootless bool, loggers log.Loggers) (run.ContainerManager, error) {
	return runcrunner.NewContainerManager(rootDir, loggers)
}
