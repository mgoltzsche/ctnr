package runcrunner

import (
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	exterrors "github.com/mgoltzsche/ctnr/pkg/errors"
	"github.com/mgoltzsche/ctnr/pkg/log"
	"github.com/mgoltzsche/ctnr/run"
	"github.com/pkg/errors"
)

type RuncProcess struct {
	args     []string
	io       run.ContainerIO
	terminal bool
	cmd      *exec.Cmd
	mutex    *sync.Mutex
	wait     *sync.WaitGroup
	debug    log.Logger
	err      error
}

func NewRuncProcess(args []string, terminal bool, io run.ContainerIO, debug log.Logger) (c *RuncProcess) {
	return &RuncProcess{
		args:     args,
		io:       io,
		terminal: terminal,
		mutex:    &sync.Mutex{},
		wait:     &sync.WaitGroup{},
		debug:    debug,
	}
}

func (c *RuncProcess) Start() (err error) {
	c.debug.Printf("Starting process %p: %s", c, strings.Join(c.args, " "))
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if c.cmd != nil {
		return errors.Errorf("start: process already started (%+v)", c.args)
	}

	c.err = nil
	c.cmd = exec.Command(c.args[0], c.args[1:]...)
	c.cmd.Stdout = c.io.Stdout
	c.cmd.Stderr = c.io.Stderr
	c.cmd.Stdin = c.io.Stdin

	if c.terminal && c.cmd.Stdin == nil {
		c.cmd.Stdin = os.Stdin
	}

	if !c.terminal {
		// Run in separate process group to be able to control orderly shutdown
		c.cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	}

	if err = c.cmd.Start(); err != nil {
		return errors.Wrapf(err, "exec %+v", c.args)
	}

	c.wait.Add(1)
	go c.cmdWait()
	return
}

func (c *RuncProcess) cmdWait() {
	defer c.wait.Done()
	argStr := strings.Join(c.args, " ")
	c.err = run.NewExitError(c.cmd.Wait(), argStr)
	c.debug.Printf("Process %p terminated", c)
}

func (c *RuncProcess) Stop() {
	if c.cmd == nil {
		return
	}

	go c.stop()
}

func (c *RuncProcess) stop() {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if c.cmd == nil {
		return
	}

	if c.cmd.Process != nil && c.cmd.ProcessState != nil && !c.cmd.ProcessState.Exited() {
		// Terminate process orderly
		c.debug.Println("Terminating process...")
		c.cmd.Process.Signal(syscall.SIGINT)
	}

	quit := make(chan error, 1)
	go func() {
		quit <- c.Wait()
	}()
	var err, ex error
	select {
	case <-time.After(time.Duration(10000000)): // TODO: read value from OCI runtime configuration
		// Kill process after timeout
		if c.cmd.Process != nil {
			c.debug.Println("Killing process since stop timeout exceeded")
			e := c.cmd.Process.Kill()
			if e != nil && c.cmd.ProcessState != nil && !c.cmd.ProcessState.Exited() {
				err = errors.Wrapf(e, "stop: failed to kill pid %d (%+v)", c.cmd.ProcessState.Pid(), c.args)
			}
			c.Wait()
		}
		ex = <-quit
	case ex = <-quit:
	}
	close(quit)
	c.cmd = nil
	c.err = exterrors.Append(ex, err)
	return
}

func (c *RuncProcess) Wait() error {
	c.wait.Wait()
	return c.err
}

func (c *RuncProcess) Close() (err error) {
	c.Stop()
	return c.Wait()
}
