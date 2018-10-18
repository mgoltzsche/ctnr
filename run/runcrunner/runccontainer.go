package runcrunner

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"

	exterrors "github.com/mgoltzsche/ctnr/pkg/errors"
	"github.com/mgoltzsche/ctnr/pkg/log"
	"github.com/mgoltzsche/ctnr/run"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/pkg/errors"
)

type RuncContainer struct {
	io             run.ContainerIO
	id             string
	bundle         run.ContainerBundle
	rootfs         string
	noNewKeyring   bool
	noPivot        bool
	destroyOnClose bool
	rootDir        string
	process        *RuncProcess
	created        bool
	debug          log.FieldLogger
	err            error
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
	c = &RuncContainer{
		id:             id,
		io:             cfg.Io,
		bundle:         cfg.Bundle,
		rootfs:         filepath.Join(cfg.Bundle.Dir(), spec.Root.Path),
		noPivot:        cfg.NoPivotRoot,
		noNewKeyring:   cfg.NoNewKeyring,
		destroyOnClose: cfg.DestroyOnClose,
		rootDir:        rootDir,
		debug:          debug.WithField("id", id),
	}

	// Create process
	c.process = NewRuncProcess(c.runcCreateArgs("run", "--bundle", c.bundle.Dir(), c.ID()), spec.Process.Terminal, cfg.Io, c.debug)
	return
}

func (c *RuncContainer) Close() (err error) {
	c.Stop()
	err = c.Wait()
	if c.destroyOnClose {
		err = exterrors.Append(err, c.destroy())
	}
	err = exterrors.Append(err, c.bundle.Close())
	return
}

func (c *RuncContainer) ID() string {
	return c.id
}

func (c *RuncContainer) Rootfs() string {
	return c.rootfs
}

func (c *RuncContainer) Start() (err error) {
	c.debug.Println("Starting container")
	return c.process.Start()
}

func (c *RuncContainer) runcCreateArgs(cmd string, a ...string) []string {
	args := c.runcArgs(cmd)
	if c.noPivot {
		args = append(args, "--no-pivot")
	}
	if c.noNewKeyring {
		args = append(args, "--no-new-keyring")
	}
	return append(args, a...)
}

func (c *RuncContainer) runcArgs(a ...string) []string {
	return append(append(make([]string, 0, len(a)+3), "runc", "--root", c.rootDir), a...)
}

func (c *RuncContainer) Stop() {
	c.process.Stop()
}

func (c *RuncContainer) Wait() error {
	return c.process.Wait()
}

func (c *RuncContainer) Destroy() (err error) {
	err = c.Close()
	return exterrors.Append(err, c.destroy())
}

func (c *RuncContainer) destroy() (err error) {
	c.debug.Println("Destroying container")
	return c.run("", "runc", "--root", c.rootDir, "delete", c.ID()) // TODO: Add --force option
}

func (c *RuncContainer) run(args ...string) (err error) {
	var stderr bytes.Buffer
	p := exec.Command(args[0], args[1:]...)
	p.Stderr = &stderr
	err = p.Run()
	return errors.Wrapf(err, "exec %+v (stderr: %s)", args, stderr.String())
}

func (c *RuncContainer) Exec(process *specs.Process, io run.ContainerIO) (proc run.Process, err error) {
	// Create container if not exists
	exists, err := c.exists()
	if err != nil {
		return nil, errors.WithMessage(err, "exec")
	}
	if !exists {
		if err = c.create(); err != nil {
			return nil, err
		}
	}

	// Start process
	args := c.runcArgs("exec", "--cwd", process.Cwd)
	if process.Terminal {
		args = append(args, "-t")
	}
	args = append(args, "-u", fmt.Sprintf("%d:%d", process.User.UID, process.User.GID))
	for _, envLine := range process.Env {
		args = append(args, "-e", envLine)
	}
	if process.SelinuxLabel != "" {
		args = append(args, "--process-label", process.SelinuxLabel)
	}
	if process.ApparmorProfile != "" {
		args = append(args, "--apparmor", process.ApparmorProfile)
	}
	if process.NoNewPrivileges {
		args = append(args, "--no-new-privs")
	}
	args = append(args, c.ID())
	args = append(args, process.Args...)
	p := NewRuncProcess(args, process.Terminal, io, c.debug)
	return p, p.Start()
}

func (c *RuncContainer) create() (err error) {
	c.debug.Println("Creating container")
	args := c.runcCreateArgs("create", "--bundle", c.bundle.Dir(), c.ID())
	p := exec.Command(args[0], args[1:]...)
	var wg sync.WaitGroup
	stderr, err := p.StderrPipe()
	if err != nil {
		return
	}
	stdout, err := p.StdoutPipe()
	if err != nil {
		return
	}
	wg.Add(2)
	go copyIO(stderr, os.Stderr, &wg)
	go copyIO(stdout, os.Stdout, &wg)
	if err = p.Run(); err == nil {
		c.created = true
	}
	wg.Wait()
	return
}

func copyIO(r io.ReadCloser, w io.Writer, wg *sync.WaitGroup) {
	defer wg.Done()
	io.Copy(w, r)
	r.Close()
}

func (c *RuncContainer) exists() (r bool, err error) {
	if c.created {
		return true, nil
	}
	err = c.run(c.runcArgs("state", c.ID())...)
	if exiterr, ok := errors.Cause(err).(*exec.ExitError); ok {
		if status, ok := exiterr.Sys().(syscall.WaitStatus); ok {
			switch status.ExitStatus() {
			case 0:
				return true, nil
			case 1:
				return false, nil
			default:
				return false, errors.New(exiterr.Error() + ", stderr: " + string(exiterr.Stderr))
			}
		}
	}
	return
}
