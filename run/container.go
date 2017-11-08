package run

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/mgoltzsche/cntnr/log"
	rspecs "github.com/opencontainers/runtime-spec/specs-go"
)

type ContainerBundle interface {
	Dir() string
	Spec() rspecs.Spec
	Close() error
}

type Container interface {
	ID() string
	Start() error
	Stop()
	Wait() error
	Close() error
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

func multiError(ex, err error) error {
	if err == nil {
		err = ex
	} else if ex != nil {
		if exiterr, ok := ex.(*ExitError); ok {
			err = &ExitError{exiterr.status, err}
		} else {
			err = fmt.Errorf("%s, await container termination: %s", err, ex)
		}
	}
	return err
}

type RuncContainer struct {
	Stdin   io.Reader
	Stdout  io.Writer
	Stderr  io.Writer
	id      string
	bundle  ContainerBundle
	rootDir string
	cmd     *exec.Cmd
	mutex   *sync.Mutex
	wait    *sync.WaitGroup
	debug   log.Logger
	err     error
}

func NewRuncContainer(id string, bundle ContainerBundle, rootDir string, debug log.Logger) *RuncContainer {
	if id == "" {
		id = GenerateId()
	}
	return &RuncContainer{
		Stdout:  os.Stdout,
		Stderr:  os.Stderr,
		id:      id,
		bundle:  bundle,
		rootDir: rootDir,
		mutex:   &sync.Mutex{},
		wait:    &sync.WaitGroup{},
		debug:   debug,
	}
}

func (c *RuncContainer) ID() string {
	return c.id
}

func (c *RuncContainer) Start() (err error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if c.cmd != nil {
		return fmt.Errorf("start %q: container already started", c.ID())
	}

	c.err = nil
	c.cmd = exec.Command("runc", "--root", c.rootDir, "run", c.ID())
	c.cmd.Dir = c.bundle.Dir()
	c.cmd.Stdout = c.Stdout
	c.cmd.Stderr = c.Stderr
	c.cmd.Stdin = c.Stdin

	if c.bundle.Spec().Process.Terminal && c.cmd.Stdin == nil {
		c.cmd.Stdin = os.Stdin
	}

	if !c.bundle.Spec().Process.Terminal {
		// Run in separate process group to be able to control orderly shutdown
		c.cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	}

	if err = c.cmd.Start(); err != nil {
		return fmt.Errorf("start %q: %s", c.ID(), err)
	}

	c.wait.Add(1)
	go c.cmdWait()

	return
}

func (c *RuncContainer) cmdWait() {
	defer c.wait.Done()
	c.err = exitError(c.cmd.Wait())
	c.debug.Printf("Container %q terminated", c.ID())
}

func (c *RuncContainer) Stop() {
	if c.cmd == nil {
		return
	}

	go c.stop()
}

func (c *RuncContainer) stop() {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if c.cmd.Process != nil {
		// Terminate container orderly
		c.debug.Printf("Terminating container %q...", c.ID())
		c.cmd.Process.Signal(syscall.SIGINT)
	}

	quit := make(chan error, 1)
	go func() {
		quit <- c.Wait()
	}()
	var err, ex error
	select {
	case <-time.After(time.Duration(10000000)): // TODO: read value from OCI runtime configuration
		// Kill container after timeout
		if c.cmd.Process != nil {
			c.debug.Printf("Killing container %q since stop timeout exceeded", c.ID())
			e := c.cmd.Process.Kill()
			if e != nil && c.cmd.ProcessState != nil && !c.cmd.ProcessState.Exited() {
				err = fmt.Errorf("stop: container %q has been killed since it did not respond: %s", c.ID(), e)
			}
			c.Wait()
		}
		ex = <-quit
	case ex = <-quit:
	}
	close(quit)
	c.cmd = nil
	c.err = multiError(ex, err)
	return
}

func (c *RuncContainer) Wait() error {
	c.wait.Wait()
	return c.err
}

func (c *RuncContainer) Close() error {
	c.Stop()
	err := c.Wait()
	if e := c.bundle.Close(); e != nil {
		err = multiError(err, e)
	}
	return err
}
