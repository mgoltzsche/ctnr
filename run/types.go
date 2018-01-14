package run

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"

	"github.com/hashicorp/go-multierror"
	rspecs "github.com/opencontainers/runtime-spec/specs-go"
)

type ContainerBundle interface {
	ID() string
	Dir() string
	Spec() (*rspecs.Spec, error)
	Close() error
}

type ContainerConfig struct {
	Id           string
	Bundle       ContainerBundle
	Io           ContainerIO
	NoPivotRoot  bool
	NoNewKeyring bool
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
	List() ([]ContainerInfo, error)
	Kill(id, signal string, all bool) error
}

type Container interface {
	ID() string
	Start() error
	Stop()
	Wait() error
	Close() error
}

type ContainerInfo struct {
	ID     string
	Status string
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

func NewExitError(err error) error {
	if exiterr, ok := err.(*exec.ExitError); ok {
		if status, ok := exiterr.Sys().(syscall.WaitStatus); ok {
			return &ExitError{status.ExitStatus(), nil}
		}
	}
	return err
}

func WrapExitError(ex, err error) error {
	if err == nil {
		err = ex
	} else if ex != nil {
		if exiterr, ok := ex.(*ExitError); ok {
			err = &ExitError{exiterr.status, multierror.Append(ex, err)}
		} else {
			err = fmt.Errorf("%s, await container termination: %s", err, ex)
		}
	}
	return err
}
