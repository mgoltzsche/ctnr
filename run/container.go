package run

import (
	"fmt"
	"github.com/mgoltzsche/cntnr/log"
	//"github.com/mgoltzsche/cntnr/net"
	specs "github.com/opencontainers/runtime-spec/specs-go"
	"os"
	"os/exec"
	"syscall"
	"time"
)

type Container interface {
	ID() string
	Start() error
	Stop() error
	Wait() error
	Delete() error
}

type RuncContainer struct {
	id    string
	spec  *specs.Spec
	cmd   *exec.Cmd
	error log.Logger
	debug log.Logger
}

func (c *RuncContainer) ID() string {
	return c.id
}

func (c *RuncContainer) Start() (err error) {
	err = c.cmd.Start()
	if err != nil {
		err = fmt.Errorf("Container %q start failed: %v", c.id, err)
	}
	return
}

func (c *RuncContainer) Wait() error {
	err := c.cmd.Wait()
	if err != nil {
		err = fmt.Errorf("Container %q terminated: %v", c.id, err)
	}
	return err
}

func (c *RuncContainer) Stop() (err error) {
	if c.cmd.Process != nil {
		c.debug.Printf("Terminating container %q...", c.id)
		c.cmd.Process.Signal(syscall.SIGINT)
	}

	quit := make(chan bool, 1)
	go func() {
		c.cmd.Wait()
		quit <- true
	}()
	select {
	case <-time.After(time.Duration(10000000)): // TODO: read value from OCI runtime configuration
		c.error.Printf("Killing container %q since stop timeout exceeded", c.id)
		if c.cmd.Process != nil {
			err = c.cmd.Process.Kill()
			if err != nil && !c.cmd.ProcessState.Exited() {
				err = fmt.Errorf("Failed to kill container %s: %s", c.id, err)
			}
			c.cmd.Wait()
		}
		<-quit
	case <-quit:
	}
	close(quit)

	return err
}

func (c *RuncContainer) Delete() error {
	//return os.RemoveAll(c.cmd.Dir)
	return nil
}

func NewContainer(id, runtimeBundleDir, rootDir string, spec *specs.Spec, bindStdin bool, error, debug log.Logger) (Container, error) {
	c := exec.Command("runc", "--root", rootDir, "run", "-bundle", runtimeBundleDir, id)
	c.Dir = runtimeBundleDir
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr

	if bindStdin || spec.Process.Terminal {
		c.Stdin = os.Stdin
	}

	if !spec.Process.Terminal {
		c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} // Run in separate process group to be able to control orderly shutdown
	}

	return &RuncContainer{id, spec, c, error, debug}, nil
}
