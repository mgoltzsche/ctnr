package run

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/mgoltzsche/cntnr/log"
)

type ContainerGroup struct {
	runners map[string]Container
	debug   log.Logger
}

func NewContainerGroup(debug log.Logger) *ContainerGroup {
	return &ContainerGroup{map[string]Container{}, debug}
}

func (m *ContainerGroup) Close() (err error) {
	for _, c := range m.runners {
		c.Stop()
	}
	for _, c := range m.runners {
		if e := c.Wait(); e != nil {
			if err == nil {
				err = e
			} else {
				err = WrapExitError(e, err)
			}
		}
	}
	m.runners = map[string]Container{}
	return err
}

func (m *ContainerGroup) Deploy(c Container) error {
	err := c.Start()
	if err == nil {
		m.runners[c.ID()] = c
	}
	return err
}

func (m *ContainerGroup) Wait() (err error) {
	for _, c := range m.runners {
		e := c.Wait()
		if e != nil {
			m.debug.Println(e)
			if err == nil {
				err = e
			}
		}
	}
	m.runners = map[string]Container{}
	return err
}

func (m *ContainerGroup) HandleSignals() {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM, syscall.SIGKILL)
	go func() {
		<-sigs
		err := m.Close()
		if err != nil {
			os.Stderr.WriteString(fmt.Sprintf("Failed to stop: %s\n", err))
		}
	}()
}
