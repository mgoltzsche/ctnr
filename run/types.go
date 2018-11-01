package run

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"

	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/pkg/errors"
)

type ContainerBundle interface {
	ID() string
	Dir() string
	Spec() (*specs.Spec, error)
	Close() error
}

type ContainerConfig struct {
	Id             string
	Bundle         ContainerBundle
	Io             ContainerIO
	NoPivotRoot    bool
	NoNewKeyring   bool
	DestroyOnClose bool
}

type ContainerIO struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

func NewStdContainerIO() ContainerIO {
	return ContainerIO{
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	}
}

type ContainerManager interface {
	NewContainer(cfg *ContainerConfig) (Container, error)
	Get(id string) (Container, error)
	List() ([]ContainerInfo, error)
	Kill(id string, signal os.Signal, all bool) error
	Exist(id string) (bool, error)
}

type Container interface {
	ID() string
	Rootfs() string
	Start() error
	// TODO: expose process
	Exec(*specs.Process, ContainerIO) (Process, error)
	Destroy() error
	Process
}

type Process interface {
	Wait() error
	Stop()
	Close() error
}

type ContainerInfo struct {
	ID     string
	Status string
}

type ExitError struct {
	containerId string
	code        int
	cause       error
}

func (e *ExitError) Code() int {
	return e.code
}

func (e *ExitError) ContainerID() string {
	return e.containerId
}

func (e *ExitError) Error() string {
	return e.cause.Error()
}

func (e *ExitError) Format(s fmt.State, verb rune) {
	type formatter interface {
		Format(s fmt.State, verb rune)
	}
	e.cause.(formatter).Format(s, verb)
}

func NewExitError(err error, containerId string) error {
	if err == nil {
		return nil
	}
	if exiterr, ok := err.(*exec.ExitError); ok {
		if status, ok := exiterr.Sys().(syscall.WaitStatus); ok {
			code := status.ExitStatus()
			return &ExitError{containerId, code, errors.New(fmt.Sprintf("%s terminated: exit code %d", containerId, code))}
		}
	}
	return errors.New(err.Error())
}

func FindExitError(err error) *ExitError {
	if err != nil {
		type causer interface {
			Cause() error
		}
		if e, ok := err.(*ExitError); ok {
			return e
		}
		if e, ok := err.(causer); ok && e.Cause() != nil {
			return FindExitError(e.Cause())
		}
	}
	return nil
}
