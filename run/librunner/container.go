package librunner

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"

	exterrors "github.com/mgoltzsche/cntnr/pkg/errors"
	"github.com/mgoltzsche/cntnr/pkg/log"
	"github.com/mgoltzsche/cntnr/run"
	"github.com/opencontainers/runc/libcontainer"
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
	process        *Process
	container      libcontainer.Container
	id             string
	destroyOnClose bool
	bundle         io.Closer
	log            log.Loggers
}

// TODO: Add to ContainerManager interface
func LoadContainer(id string, factory libcontainer.Factory, loggers log.Loggers) (r *Container, err error) {
	c, err := factory.Load(id)
	return &Container{
		id:        c.ID(),
		container: c,
		log:       loggers,
	}, err
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
	defer exterrors.Wrapd(&err, "new container")

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
		return nil, errors.New(err.Error())
	}

	// Must change to bundle dir because CreateLibcontainerConfig assumes it is in the bundle directory
	if err = os.Chdir(cfg.Bundle.Dir()); err != nil {
		return nil, errors.New("change to bundle directory: " + err.Error())
	}
	defer func() {
		if e := os.Chdir(orgwd); e != nil {
			e = errors.New("change back from bundle to previous directory: " + e.Error())
			err = exterrors.Append(err, e)
		}
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
		return nil, errors.New(err.Error())
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
		return nil, errors.New(err.Error())
	}

	r = &Container{
		container:      container,
		id:             id,
		bundle:         cfg.Bundle,
		destroyOnClose: cfg.DestroyOnClose,
		log:            loggers,
	}
	r.process, err = NewProcess(r, spec.Process, cfg.Io, loggers)
	return
}

func (c *Container) ID() string {
	return c.id
}

func (c *Container) Rootfs() string {
	return c.container.Config().Rootfs
}

// Prepare and start the container process from spec and with stdio
func (c *Container) Start() (err error) {
	c.log.Debug.Println("Starting container")
	return c.process.Start()
}

func (c *Container) Stop() {
	c.log.Debug.Println("Stopping container")
	if p := c.process; p != nil {
		p.Stop()
	}
}

func (c *Container) Exec(process *specs.Process, io run.ContainerIO) (err error) {
	p, err := NewProcess(c, process, io, c.log)
	if err = p.Start(); err == nil {
		err = p.Wait()
	}
	return
}

// Waits for the container process to terminate and returns the process' error if any
func (c *Container) Wait() (err error) {
	if p := c.process; p != nil {
		err = p.Wait()
	}
	return
}

func (c *Container) Destroy() (err error) {
	c.log.Debug.Println("Destroying container")
	cc := c.container
	if cc != nil {
		err = exterrors.Append(err, cc.Destroy())
		c.container = nil
	}
	return
}

func (c *Container) Close() (err error) {
	c.log.Debug.Println("Closing container")

	// Close process
	p := c.process
	if p != nil {
		err = p.Close()
		c.process = nil
	}

	// Destroy container
	if c.destroyOnClose {
		err = exterrors.Append(err, c.Destroy())
	}

	// Release bundle
	b := c.bundle
	if b != nil {
		err = exterrors.Append(err, b.Close())
		c.bundle = nil
	}
	return
}
