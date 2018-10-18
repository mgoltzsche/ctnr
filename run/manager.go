package run

import (
	"os"
	"os/signal"
	"syscall"

	exterrors "github.com/mgoltzsche/ctnr/pkg/errors"
	"github.com/mgoltzsche/ctnr/pkg/log"
)

type ContainerGroup struct {
	runners []Container
	debug   log.Logger
	err     error
}

func NewContainerGroup(debug log.Logger) *ContainerGroup {
	return &ContainerGroup{nil, debug, nil}
}

func (m *ContainerGroup) Close() (err error) {
	m.Stop()
	err = m.err
	for _, c := range m.runners {
		err = exterrors.Append(err, c.Close())
	}
	m.runners = nil
	return err
}

func (m *ContainerGroup) Add(c Container) {
	m.runners = append(m.runners, c)
}

func (m *ContainerGroup) Start() {
	if m.err != nil {
		return
	}

	for i, c := range m.runners {
		m.err = c.Start()
		if m.err != nil {
			m.debug.Println("start:", m.err)
			for _, sc := range m.runners[0:i] {
				sc.Stop()
				m.err = exterrors.Append(m.err, sc.Wait())
			}
			return
		}
	}
	return
}

func (m *ContainerGroup) Stop() {
	for _, c := range m.runners {
		c.Stop()
	}
}

func (m *ContainerGroup) Wait() {
	if m.err != nil {
		return
	}

	m.handleSignals()

	for _, c := range m.runners {
		m.err = exterrors.Append(m.err, c.Wait())
	}
	return
}

// TODO: close signal channel if there is a use case where the process is not terminated afterwards
func (m *ContainerGroup) handleSignals() {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM, syscall.SIGKILL)
	go func() {
		<-sigs
		m.Stop()
	}()
}
