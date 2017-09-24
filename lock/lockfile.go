package store

import (
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/nightlyone/lockfile"
)

type Lockfile struct {
	lockfile lockfile.Lockfile
	timeout  time.Duration
	mutex    *sync.Mutex
}

func NewLockFile(file string, retryTimeout time.Duration) (Lockfile, error) {
	l, err := lockfile.New(file)
	return Lockfile{l, retryTimeout, &sync.Mutex{}}, err
}

func (l *Lockfile) Lock() error {
	l.mutex.Lock()
	// TODO: make cancelable background operation
	maxRetries := l.timeout.Seconds()
	var n float64
	for {
		if l.lockfile.TryLock() == nil {
			return nil
		}
		n++
		if n > maxRetries {
			return fmt.Errorf("lock %s: timed out", string(l.lockfile))
		}
		time.Sleep(time.Duration(1000000000))
	}
	return nil
}

func (l *Lockfile) Unlock() error {
	defer l.mutex.Unlock()
	return l.lockfile.Unlock()
}

func (l *Lockfile) IsLocked() (bool, error) {
	_, err := os.Stat(string(l.lockfile))
	if os.IsNotExist(err) {
		return false, nil
	} else {
		return true, err
	}
}
