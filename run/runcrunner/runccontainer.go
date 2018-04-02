package runcrunner

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	exterrors "github.com/mgoltzsche/cntnr/pkg/errors"
	"github.com/mgoltzsche/cntnr/pkg/log"
	"github.com/mgoltzsche/cntnr/run"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/pkg/errors"
)

type RuncContainer struct {
	io           run.ContainerIO
	id           string
	bundle       run.ContainerBundle
	rootfs       string
	noNewKeyring bool
	noPivot      bool
	rootDir      string
	cmd          *exec.Cmd
	mutex        *sync.Mutex
	wait         *sync.WaitGroup
	debug        log.Logger
	err          error
}

func NewRuncContainer(cfg *run.ContainerConfig, rootDir string, debug log.FieldLogger) (c *RuncContainer, err error) {
	id := cfg.Id
	if id == "" {
		if id = cfg.Bundle.ID(); id == "" {
			panic("no container ID provided and bundle ID is empty")
		}
	}

	spec, err := cfg.Bundle.Spec()
	if err != nil {
		return nil, errors.Wrapf(err, "new container %q", id)
	}

	// TODO: handle config option destroyOnClose
	return &RuncContainer{
		id:           id,
		io:           cfg.Io,
		bundle:       cfg.Bundle,
		rootfs:       filepath.Join(cfg.Bundle.Dir(), spec.Root.Path),
		noPivot:      cfg.NoPivotRoot,
		noNewKeyring: cfg.NoNewKeyring,
		rootDir:      rootDir,
		mutex:        &sync.Mutex{},
		wait:         &sync.WaitGroup{},
		debug:        debug.WithField("id", id),
	}, nil
}

func (c *RuncContainer) ID() string {
	return c.id
}

func (c *RuncContainer) Rootfs() string {
	return c.rootfs
}

func (c *RuncContainer) Start() (err error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if c.cmd != nil {
		return errors.Errorf("start %q: container already started", c.ID())
	}

	spec, err := c.bundle.Spec()
	if err != nil {
		return errors.Wrapf(err, "start %q: load bundle spec", c.ID())
	}

	c.err = nil
	args := append(make([]string, 0, 5), "--root", c.rootDir)
	if c.noPivot {
		args = append(args, "--no-pivot")
	}
	if c.noNewKeyring {
		args = append(args, "--no-new-keyring")
	}
	args = append(args, "run", c.ID())
	c.cmd = exec.Command("runc", args...)
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
		return errors.Wrapf(err, "start %q", c.ID())
	}

	c.wait.Add(1)
	go c.cmdWait()

	return
}

func (c *RuncContainer) cmdWait() {
	defer c.wait.Done()
	c.err = run.NewExitError(c.cmd.Wait(), c.ID())
	c.debug.Println("Container terminated")
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

	if c.cmd == nil {
		return
	}

	if c.cmd.Process != nil {
		// Terminate container orderly
		c.debug.Println("Terminating container...")
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
			c.debug.Println("Killing container since stop timeout exceeded")
			e := c.cmd.Process.Kill()
			if e != nil && c.cmd.ProcessState != nil && !c.cmd.ProcessState.Exited() {
				err = errors.Wrapf(e, "stop: container %q has been killed since it did not respond", c.ID())
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

func (c *RuncContainer) Wait() error {
	c.wait.Wait()
	return c.err
}

func (c *RuncContainer) Destroy() (err error) {
	var stdout, stderr bytes.Buffer
	e := c.Close()
	cmd := exec.Command("runc", "--root", c.rootDir, "delete", c.ID()) // TODO: Add --force option
	cmd.Dir = c.bundle.Dir()
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	if err != nil {
		outStr, errStr := strings.Trim(string(stdout.Bytes()), "\n"), strings.Trim(string(stderr.Bytes()), "\n")
		err = errors.Errorf("runc delete: %s\n  out: %s\n  err: %s", err, outStr, errStr)
	}
	return exterrors.Append(err, e)
}

// TODO: implement model to runc parameter transformation
func (c *RuncContainer) Exec(process *specs.Process, io run.ContainerIO) (err error) {
	panic("TODO: implement")
}

func (c *RuncContainer) Close() (err error) {
	c.Stop()
	err = c.Wait()
	err = exterrors.Append(err, c.bundle.Close())
	return
}
