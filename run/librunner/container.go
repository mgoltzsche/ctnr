package librunner

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/mgoltzsche/cntnr/log"
	"github.com/mgoltzsche/cntnr/run"
	"github.com/opencontainers/runc/libcontainer"
	_ "github.com/opencontainers/runc/libcontainer/nsenter"
	"github.com/opencontainers/runc/libcontainer/specconv"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/pkg/errors"
	"golang.org/x/crypto/ssh/terminal"
)

func init() {
	if len(os.Args) > 1 && os.Args[1] == "init" {
		runtime.GOMAXPROCS(1)
		runtime.LockOSThread()
		factory, _ := libcontainer.New("")
		if err := factory.StartInitialization(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: libcontainer factory initialization: %s\n", err)
			os.Exit(1)
		}
		panic("factory initialization should block further execution - this should never be executed")
	}
}

type Container struct {
	container libcontainer.Container
	process   *libcontainer.Process
	io        run.ContainerIO
	id        string
	bundle    run.ContainerBundle
	spec      *specs.Spec
	mutex     *sync.Mutex
	wait      *sync.WaitGroup
	debug     log.Logger
	err       error
}

func NewContainer(id string, bundle run.ContainerBundle, ioe run.ContainerIO, rootless bool, factory libcontainer.Factory, debug log.Logger) (r *Container, err error) {
	if id == "" {
		if id = bundle.ID(); id == "" {
			panic("no container ID provided and bundle ID is empty")
		}
	}

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	defer func() {
		if err != nil {
			err = errors.Wrap(err, "new container")
		}
	}()

	spec, err := bundle.Spec()
	if err != nil {
		return
	}
	if spec.Process == nil {
		return nil, fmt.Errorf("bundle spec declares no process to run")
	}
	orgwd, err := os.Getwd()
	if err != nil {
		return
	}
	if err = os.Chdir(bundle.Dir()); err != nil {
		return nil, fmt.Errorf("change to bundle directory: %s", err)
	}
	defer func() {
		if e := os.Chdir(orgwd); e != nil {
			err = fmt.Errorf("change back from bundle to previous directory: %s", e)
		}
	}()

	config, err := specconv.CreateLibcontainerConfig(&specconv.CreateOpts{
		CgroupName:       id,
		UseSystemdCgroup: false,
		NoPivotRoot:      false,
		NoNewKeyring:     false,
		Spec:             spec,
		Rootless:         rootless,
	})
	if err != nil {
		return
	}
	if spec.Root != nil {
		config.Rootfs = filepath.Join(bundle.Dir(), spec.Root.Path)
	}
	container, err := factory.Create(id, config)
	if err != nil {
		return
	}

	r = &Container{
		container: container,
		id:        id,
		io:        ioe,
		bundle:    bundle,
		spec:      spec,
		mutex:     &sync.Mutex{},
		wait:      &sync.WaitGroup{},
		debug:     debug,
	}

	return
}

func (c *Container) ID() string {
	return c.id
}

func (c *Container) Start() (err error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if c.process != nil {
		return fmt.Errorf("start %q: container already started", c.ID())
	}

	// Create container process
	c.process = &libcontainer.Process{
		Args:             c.spec.Process.Args,
		Env:              c.spec.Process.Env,
		User:             strconv.Itoa(int(c.spec.Process.User.UID)),
		AdditionalGroups: ints2strings(c.spec.Process.User.AdditionalGids),
		Cwd:              c.spec.Process.Cwd,
		Stdout:           c.io.Stdout,
		Stderr:           c.io.Stderr,
		Stdin:            c.io.Stdin,
	}
	if c.spec.Process.Terminal {
		if c.process.Stdin == nil {
			c.process.Stdin = os.Stdin
		}
		if !terminal.IsTerminal(int(os.Stdin.Fd())) || !terminal.IsTerminal(int(os.Stdout.Fd())) {
			return fmt.Errorf("terminal enabled but stdin/out is not a terminal")
		}
		// TODO: set pty
	}

	// Run container process
	if err = c.container.Run(c.process); err != nil {
		return fmt.Errorf("start %q: spawn main process: %s", c.ID(), err)
	}

	c.wait.Add(1)
	go c.processWait()

	return
}

func (c *Container) processWait() {
	defer c.wait.Done()
	_, err := c.process.Wait()
	c.err = run.NewExitError(err)
	c.debug.Printf("Container %q terminated", c.ID())
}

func (c *Container) Stop() {
	if c.process == nil {
		return
	}

	go c.stop()
}

func (c *Container) stop() {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if c.process == nil {
		return
	}

	// Terminate container orderly
	c.debug.Printf("Terminating container %q...", c.ID())
	if err := c.container.Signal(syscall.SIGINT, false); err != nil {
		c.debug.Printf("Failed to send SIGINT to container %q: %s", c.ID(), err)
	}

	quit := make(chan error, 1)
	go func() {
		quit <- c.Wait()
	}()
	var err, ex error
	select {
	case <-time.After(time.Duration(10000000)): // TODO: read value from OCI runtime configuration
		// Kill container after timeout
		if c.process != nil {
			c.debug.Printf("Killing container %q since stop timeout exceeded", c.ID())
			if err = c.container.Signal(syscall.SIGKILL, true); err != nil {
				err = fmt.Errorf("stop: killing container %q: %s", c.ID(), err)
			}
			c.Wait()
		}
		ex = <-quit
	case ex = <-quit:
	}
	close(quit)
	c.process = nil
	c.err = run.WrapExitError(ex, err)
	return
}

func (c *Container) Wait() error {
	c.wait.Wait()
	return c.err
}

func (c *Container) Close() error {
	c.Stop()
	err := c.Wait()
	if e := c.bundle.Close(); e != nil {
		err = run.WrapExitError(err, e)
	}
	if derr := c.container.Destroy(); derr != nil {
		err = run.WrapExitError(err, derr)
	}
	c.container = nil
	return err
}

func ints2strings(a []uint32) (r []string) {
	r = make([]string, len(a))
	for i, e := range a {
		r[i] = strconv.Itoa(int(e))
	}
	return
}
