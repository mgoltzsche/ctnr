package run

import (
	"fmt"
	"github.com/mgoltzsche/cntnr/log"
	"os"
	"os/signal"
	"syscall"
)

type ContainerManager struct {
	runners map[string]Container
	debug   log.Logger
}

func NewContainerManager(debug log.Logger) *ContainerManager {
	return &ContainerManager{map[string]Container{}, debug}
}

func (m *ContainerManager) Deploy(c Container) error {
	err := c.Start()
	if err == nil {
		m.runners[c.ID()] = c
	}
	return err
}

func (m *ContainerManager) Stop() (err error) {
	for _, c := range m.runners {
		e := c.Stop()
		if e != nil {
			m.debug.Printf("Failed to stop container %s: %v", c.ID(), err)
			if err == nil {
				err = e
			}
		}
	}
	return err
}

func (m *ContainerManager) Wait() (err error) {
	for _, c := range m.runners {
		e := c.Wait()
		if e != nil {
			m.debug.Printf("Failed to wait for container %s: %v", c.ID(), err)
			if err == nil {
				err = e
			}
		}
	}
	return err
}

func (m *ContainerManager) HandleSignals() {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM, syscall.SIGKILL)
	go func() {
		<-sigs
		err := m.Stop()
		if err != nil {
			os.Stderr.WriteString(fmt.Sprintf("Failed to stop: %s\n", err))
		}
	}()
}
