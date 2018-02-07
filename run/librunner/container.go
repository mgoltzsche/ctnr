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

	"github.com/coreos/go-systemd/activation"
	"github.com/mgoltzsche/cntnr/log"
	"github.com/mgoltzsche/cntnr/run"
	"github.com/opencontainers/runc/libcontainer"
	"github.com/opencontainers/runc/libcontainer/configs"
	_ "github.com/opencontainers/runc/libcontainer/nsenter"
	"github.com/opencontainers/runc/libcontainer/specconv"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/pkg/errors"
)

func init() {
	if len(os.Args) > 1 && os.Args[1] == "init" {
		// Initializes the previously created container in this new process
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
	tty       *tty
	io        run.ContainerIO
	id        string
	bundle    run.ContainerBundle
	spec      *specs.Spec
	mutex     *sync.Mutex
	wait      *sync.WaitGroup
	log       log.Loggers
	err       error
}

func NewContainer(cfg *run.ContainerConfig, rootless bool, factory libcontainer.Factory, loggers log.Loggers) (r *Container, err error) {
	id := cfg.Id
	if id == "" {
		if id = cfg.Bundle.ID(); id == "" {
			panic("no container ID provided and bundle ID is empty")
		}
	}

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	defer func() {
		err = errors.Wrap(err, "new container")
	}()

	loggers = loggers.WithField("id", id)
	loggers.Debug.Println("Creating container")

	spec, err := cfg.Bundle.Spec()
	if err != nil {
		return
	}
	if spec.Process == nil {
		return nil, errors.New("bundle spec declares no process to run")
	}
	orgwd, err := os.Getwd()
	if err != nil {
		return
	}

	// Must change to bundle dir because CreateLibcontainerConfig assumes it is in the bundle directory
	if err = os.Chdir(cfg.Bundle.Dir()); err != nil {
		return nil, errors.Wrap(err, "change to bundle directory")
	}
	defer func() {
		e := os.Chdir(orgwd)
		err = errors.Wrap(e, "change back from bundle to previous directory")
	}()

	config, err := specconv.CreateLibcontainerConfig(&specconv.CreateOpts{
		CgroupName:       id,
		UseSystemdCgroup: false, // TODO: expose as option
		NoPivotRoot:      cfg.NoPivotRoot,
		NoNewKeyring:     cfg.NoNewKeyring,
		Spec:             spec,
		Rootless:         rootless,
	})
	if err != nil {
		return
	}
	if spec.Root != nil {
		if filepath.IsAbs(spec.Root.Path) {
			config.Rootfs = spec.Root.Path
		} else {
			config.Rootfs = filepath.Join(cfg.Bundle.Dir(), spec.Root.Path)
		}
	}
	container, err := factory.Create(id, config)
	if err != nil {
		return
	}

	r = &Container{
		container: container,
		id:        id,
		io:        cfg.Io,
		bundle:    cfg.Bundle,
		spec:      spec,
		mutex:     &sync.Mutex{},
		wait:      &sync.WaitGroup{},
		log:       loggers,
	}

	return
}

func (c *Container) ID() string {
	return c.id
}

// Prepare and start the container process from spec and with stdio
func (c *Container) Start() (err error) {
	c.log.Debug.Println("Starting container")

	c.mutex.Lock()
	defer c.mutex.Unlock()

	defer func() {
		err = errors.Wrapf(err, "start %q", c.container.ID())
	}()

	if c.process != nil {
		return errors.New("container already started")
	}

	// Create container process (see https://github.com/opencontainers/runc/blob/v1.0.0-rc4/utils_linux.go: startContainer->runner.run->newProcess)
	p := c.spec.Process
	lp := &libcontainer.Process{
		Args:   p.Args,
		Env:    p.Env,
		User:   fmt.Sprintf("%d:%d", p.User.UID, p.User.GID),
		Cwd:    p.Cwd,
		Stdout: c.io.Stdout,
		Stderr: c.io.Stderr,
		Stdin:  c.io.Stdin,
	}
	for _, gid := range p.User.AdditionalGids {
		lp.AdditionalGroups = append(lp.AdditionalGroups, strconv.FormatUint(uint64(gid), 10))
	}
	if p.Capabilities != nil {
		lp.Capabilities = &configs.Capabilities{}
		lp.Capabilities.Bounding = p.Capabilities.Bounding
		lp.Capabilities.Effective = p.Capabilities.Effective
		lp.Capabilities.Inheritable = p.Capabilities.Inheritable
		lp.Capabilities.Permitted = p.Capabilities.Permitted
		lp.Capabilities.Ambient = p.Capabilities.Ambient
	}
	if p.Rlimits != nil {
		for _, rlimit := range p.Rlimits {
			rl, err := createLibContainerRlimit(rlimit)
			if err != nil {
				return err
			}
			lp.Rlimits = append(lp.Rlimits, rl)
		}
	}
	if os.Getenv("LISTEN_FDS") != "" {
		// Add systemd file descriptors
		lp.ExtraFiles = activation.Files(false)
	}

	// Configure stdIO/terminal
	tty, err := setupIO(lp, c.container, p.Terminal, false, "")
	if err != nil {
		return
	}

	// Run container process
	if err = c.container.Run(lp); err != nil {
		return errors.Wrap(err, "spawn main process")
	}
	c.process = lp
	c.tty = tty

	c.wait.Add(1)
	go c.handleProcessTermination()

	return
}

func (c *Container) handleProcessTermination() {
	defer c.wait.Done()

	// Wait for process
	_, err := c.process.Wait()

	err = run.NewExitError(err, c.ID())
	logger := c.log.Debug
	if exiterr, ok := err.(*run.ExitError); ok {
		logger = logger.WithField("status", exiterr.Status())
	}
	logger.Println("Container terminated")

	c.mutex.Lock()
	defer c.mutex.Unlock()

	// Register process error
	c.err = err
	c.process = nil

	// Release TTY
	// TODO: reject tty CLI option when process is detached
	err = c.tty.Close() // ATTENTION: call hangs when detached process and tty enabled
	c.err = run.WrapExitError(c.err, err)
	c.tty = nil
}

func (c *Container) Stop() {
	if c.process == nil {
		return
	}

	go c.stop()
}

func (c *Container) stop() bool {
	// Terminate container orderly
	c.mutex.Lock()

	if c.process == nil {
		c.mutex.Unlock()
		return false
	}

	c.log.Debug.Println("Stopping container")

	if err := c.process.Signal(syscall.SIGINT); err != nil {
		c.log.Debug.Println("Failed to send SIGINT to container:", err)
	}
	c.mutex.Unlock()

	quit := make(chan bool, 1)
	go func() {
		c.wait.Wait()
		quit <- true
	}()
	select {
	case <-time.After(time.Duration(10000000)): // TODO: read value from OCI runtime configuration
		// Kill container after timeout
		process := c.process
		if process != nil {
			c.log.Warn.Println("Killing container (stop timeout exceeded)")
			if err := c.process.Signal(syscall.SIGKILL); err != nil {
				errlog := c.log.Error
				if pid, e := c.process.Pid(); e == nil {
					errlog = errlog.WithField("pid", pid)
				}
				errlog.Println("Failed to kill container:", err)
			}
			c.wait.Wait()
		}
		<-quit
	case <-quit:
	}
	close(quit)
	return true
}

// Waits for the container process to terminate and returns the process' error if any
func (c *Container) Wait() (err error) {
	c.wait.Wait()
	c.mutex.Lock()
	defer c.mutex.Unlock()
	err = c.err
	c.err = nil
	return err
}

func (c *Container) Close() (err error) {
	c.stop()
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if c.container != nil {
		err = c.err
		if e := c.container.Destroy(); e != nil {
			err = run.WrapExitError(err, e)
		}
		if e := c.bundle.Close(); e != nil {
			err = run.WrapExitError(err, e)
		}
		c.container = nil
		c.bundle = nil
		c.err = nil
	}
	return
}
