package run

import (
	"fmt"
	"github.com/mgoltzsche/cntnr/log"
	//"github.com/mgoltzsche/cntnr/net"
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
}

type RuncContainer struct {
	id    string
	cmd   *exec.Cmd
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
		os.Stderr.WriteString(fmt.Sprintf("Killing container %q since stop timeout exceeded\n", c.id))
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
