package runcrunner

import (
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/mgoltzsche/cntnr/log"
	"github.com/mgoltzsche/cntnr/run"
)

type RuncContainer struct {
	io      run.ContainerIO
	id      string
	bundle  run.ContainerBundle
	rootDir string
	cmd     *exec.Cmd
	mutex   *sync.Mutex
	wait    *sync.WaitGroup
	debug   log.Logger
	err     error
}

func NewRuncContainer(id string, bundle run.ContainerBundle, rootDir string, ioe run.ContainerIO, debug log.Logger) *RuncContainer {
	if id == "" {
		if id = bundle.ID(); id == "" {
			panic("no container ID provided and bundle ID is empty")
		}
	}
	return &RuncContainer{
		io:      ioe,
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

	spec, err := c.bundle.Spec()
	if err != nil {
		return fmt.Errorf("start %q: could not load bundle's spec: %s", c.ID(), err)
	}

	c.err = nil
	c.cmd = exec.Command("runc", "--root", c.rootDir, "run", c.ID())
	c.cmd.Dir = c.bundle.Dir()
	c.cmd.Stdout = c.io.Stdout
	c.cmd.Stderr = c.io.Stderr
	c.cmd.Stdin = c.io.Stdin

	if spec.Process.Terminal && c.cmd.Stdin == nil {
		c.cmd.Stdin = os.Stdin
	}

	if !spec.Process.Terminal {
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
	c.err = run.NewExitError(c.cmd.Wait())
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
	c.err = run.WrapExitError(ex, err)
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
		err = run.WrapExitError(err, e)
	}
	return err
}
