package run

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/mgoltzsche/cntnr/log"
)

type Container interface {
	ID() string
	Start() error
	Stop() error
	Wait() error
}

type ExitError struct {
	status int
	cause  error
}

func (e *ExitError) Status() int {
	return e.status
}

func (e *ExitError) Error() string {
	if e.cause == nil {
		return fmt.Sprintf("container terminated: exit status %d", e.status)
	} else {
		return fmt.Sprintf("container terminated: exit status %d. error: %s", e.status, e.cause)
	}
}

func exitError(err error) error {
	if exiterr, ok := err.(*exec.ExitError); ok {
		if status, ok := exiterr.Sys().(syscall.WaitStatus); ok {
			return &ExitError{status.ExitStatus(), nil}
		}
	}
	return err
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
		err = fmt.Errorf("container %q start: %v", c.id, err)
	}
	return
}

func (c *RuncContainer) Wait() error {
	return exitError(c.cmd.Wait())
}

func (c *RuncContainer) Stop() (err error) {
	if c.cmd.Process != nil {
		c.debug.Printf("Terminating container %q...", c.id)
		c.cmd.Process.Signal(syscall.SIGINT)
	}

	quit := make(chan error, 1)
	go func() {
		quit <- exitError(c.cmd.Wait())
	}()
	var ex error
	select {
	case <-time.After(time.Duration(10000000)): // TODO: read value from OCI runtime configuration
		os.Stderr.WriteString(fmt.Sprintf("Killing container %q since stop timeout exceeded\n", c.id))
		if c.cmd.Process != nil {
			e := c.cmd.Process.Kill()
			if e != nil && c.cmd.ProcessState != nil && !c.cmd.ProcessState.Exited() {
				err = fmt.Errorf("killing container %s", c.ID(), e)
			}
			c.cmd.Wait()
		}
		ex = <-quit
	case ex = <-quit:
	}
	close(quit)

	if err == nil {
		err = ex
	} else if ex != nil {
		if exiterr, ok := ex.(*ExitError); ok {
			err = &ExitError{exiterr.status, err}
		} else {
			err = fmt.Errorf("%s, await container termination: %s", err, ex)
		}
	}
	return
}
