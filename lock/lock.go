package store

import (
	"fmt"
	"time"

	"github.com/nightlyone/lockfile"
)

type Locker interface {
	TryLock() error
	Lock() error
	Unlock() error
}

type locker struct {
	file     string
	lockfile lockfile.Lockfile
	timeout  time.Duration
}

func NewExclusiveFileLock(file string, timeout time.Duration) (Locker, error) {
	l, err := lockfile.New(file)
	return &locker{file, l, timeout}, err
}

func (l *locker) TryLock() error {
	return l.lockfile.TryLock()
}

func (l *locker) Lock() error {
	// TODO: make cancelable background operation
	maxRetries := l.timeout.Seconds()
	var n float64
	for {
		if l.TryLock() == nil {
			return nil
		}
		n++
		if n > maxRetries {
			return fmt.Errorf("lock %s: timed out", l.file)
		}
		time.Sleep(time.Duration(1000000000))
	}
	return nil
}

func (l *locker) Unlock() error {
	return l.lockfile.Unlock()
}
