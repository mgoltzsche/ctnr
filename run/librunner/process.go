package librunner

import (
	"fmt"
	"os"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/coreos/go-systemd/activation"
	exterrors "github.com/mgoltzsche/cntnr/pkg/errors"
	"github.com/mgoltzsche/cntnr/pkg/log"
	"github.com/mgoltzsche/cntnr/run"
	"github.com/opencontainers/runc/libcontainer"
	"github.com/opencontainers/runc/libcontainer/configs"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/pkg/errors"
)

type Process struct {
	args      []string
	container *Container
	process   libcontainer.Process
	io        run.ContainerIO
	tty       *tty
	terminal  bool
	running   bool
	mutex     *sync.Mutex
	wait      *sync.WaitGroup
	log       log.Loggers
	err       error
}

func NewProcess(container *Container, p *specs.Process, io run.ContainerIO, loggers log.Loggers) (r *Process, err error) {
	// Create container process (see https://github.com/opencontainers/runc/blob/v1.0.0-rc4/utils_linux.go: startContainer->runner.run->newProcess)
	r = &Process{
		args:      p.Args,
		container: container,
		io:        io,
		terminal:  p.Terminal,
		mutex:     &sync.Mutex{},
		wait:      &sync.WaitGroup{},
		log:       loggers,
		process: libcontainer.Process{
			Args:            p.Args,
			Env:             p.Env,
			User:            fmt.Sprintf("%d:%d", p.User.UID, p.User.GID),
			Cwd:             p.Cwd,
			Stdout:          io.Stdout,
			Stderr:          io.Stderr,
			Stdin:           io.Stdin,
			AppArmorProfile: p.ApparmorProfile,
			Label:           p.SelinuxLabel,
			NoNewPrivileges: &p.NoNewPrivileges,
		},
	}
	lp := r.process
	for _, gid := range p.User.AdditionalGids {
		lp.AdditionalGroups = append(lp.AdditionalGroups, strconv.FormatUint(uint64(gid), 10))
	}
	if p.Capabilities != nil {
		lp.Capabilities = &configs.Capabilities{
			Bounding:    p.Capabilities.Bounding,
			Effective:   p.Capabilities.Effective,
			Inheritable: p.Capabilities.Inheritable,
			Permitted:   p.Capabilities.Permitted,
			Ambient:     p.Capabilities.Ambient,
		}
	}
	if p.Rlimits != nil {
		for _, rlimit := range p.Rlimits {
			rl, err := createLibContainerRlimit(rlimit)
			if err != nil {
				return nil, errors.New("new process: " + err.Error())
			}
			lp.Rlimits = append(lp.Rlimits, rl)
		}
	}
	if os.Getenv("LISTEN_FDS") != "" {
		// Add systemd file descriptors
		lp.ExtraFiles = activation.Files(false)
	}
	return
}

func (p *Process) Start() (err error) {
	p.log.Debug.WithField("args", p.args).Println("Starting process")

	p.mutex.Lock()
	defer p.mutex.Unlock()
	defer exterrors.Wrapd(&err, "run process")

	if p.running {
		return errors.New("process already started")
	}

	// Configure stdIO/terminal
	p.tty, err = setupIO(&p.process, p.container.container, p.terminal, false, "")
	if err != nil {
		return
	}

	// Run container process
	if err = p.container.container.Run(&p.process); err != nil {
		p.tty.Close()
		return
	}
	p.running = true

	p.wait.Add(1)
	go p.handleTermination()
	return nil
}

func (p *Process) handleTermination() {
	defer p.wait.Done()

	// Wait for process
	_, err := p.process.Wait()

	err = run.NewExitError(err, p.container.ID())
	logger := p.log.Debug
	if exiterr, ok := err.(*run.ExitError); ok {
		logger = logger.WithField("code", exiterr.Code())
	}
	logger.WithField("args", p.args).Println("Process terminated")

	p.mutex.Lock()
	defer p.mutex.Unlock()

	// Register process error
	p.err = err

	// Release TTY
	// TODO: reject tty CLI option when process is detached and no console socket provided
	err = p.tty.Close() // ATTENTION: deadlock when detached process and tty enabled
	p.err = exterrors.Append(p.err, err)
	p.tty = nil
	p.running = false
}

func (c *Process) Stop() {
	if c.running {
		return
	}

	go c.stop()
}

func (p *Process) stop() bool {
	// Terminate container orderly
	p.mutex.Lock()

	if !p.running {
		p.mutex.Unlock()
		return false
	}

	p.log.Debug.WithField("args", p.args).Println("Stopping process")

	if err := p.process.Signal(syscall.SIGINT); err != nil {
		p.log.Debug.Println("Failed to send SIGINT to process:", err)
	}
	p.mutex.Unlock()

	quit := make(chan bool, 1)
	go func() {
		p.wait.Wait()
		quit <- true
	}()
	select {
	case <-time.After(time.Duration(10000000)): // TODO: read value from OCI runtime configuration
		// Kill container after timeout
		if p.running {
			p.log.Warn.WithField("args", p.args).Println("Killing process (stop timeout exceeded)")
			if err := p.process.Signal(syscall.SIGKILL); err != nil {
				errlog := p.log.Error
				if pid, e := p.process.Pid(); e == nil {
					errlog = errlog.WithField("pid", pid)
				}
				errlog.Println("Failed to kill process:", err)
			}
			p.wait.Wait()
		}
		<-quit
	case <-quit:
	}
	close(quit)
	return true
}

// Waits for the container process to terminate and returns the process' error if any
func (p *Process) Wait() (err error) {
	p.wait.Wait()
	p.mutex.Lock()
	defer p.mutex.Unlock()
	err = p.err
	p.err = nil
	return err
}

func (p *Process) Close() (err error) {
	p.stop()
	p.mutex.Lock()
	defer p.mutex.Unlock()
	if p.running {
		err = p.err
		p.err = nil
		if p.tty != nil {
			err = exterrors.Append(err, p.tty.Close())
			p.tty = nil
		}
	}
	return
}
