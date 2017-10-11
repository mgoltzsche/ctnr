package store

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

var _ SharedLock = &SharedDirLock{}

type SharedDirLock struct {
	dir            string
	sharedLockFile string
	lockfile       *Lockfile
	timeout        time.Duration
}

func NewSharedLock(dir string, retryTimeout time.Duration) (l SharedDirLock, err error) {
	if err = os.MkdirAll(dir, 0755); err != nil {
		err = fmt.Errorf("init lock directory: %s", err)
		return
	}
	l.dir = dir
	l.timeout = retryTimeout

	if l.lockfile, err = LockFile(filepath.Join(dir, "exclusive.lock"), time.Duration(3000000000)); err != nil {
		return
	}

	// Try to create shared lock, waiting for exclusive lock to become free or timeout
	deadline := time.Now().Add(retryTimeout)
	for {
		locked, e := l.lockfile.IsLocked()
		if e != nil {
			err = fmt.Errorf("shared lock: %s", e)
			return
		}
		if !locked {
			tmp, e := ioutil.TempFile(dir, fmt.Sprintf("sharedlock-%d-", os.Getpid()))
			if e != nil {
				err = fmt.Errorf("shared lock: %s", e)
				return
			}
			l.sharedLockFile = tmp.Name()
			tmp.Close()
			locked, e = l.lockfile.IsLocked()
			if e != nil {
				err = fmt.Errorf("shared lock: %s", e)
				return
			}
			if locked {
				if e = os.Remove(l.sharedLockFile); e != nil {
					fmt.Fprintf(os.Stderr, "Error: remove shared lock file: %s\n", e)
				}
			} else {
				break
			}
			if time.Now().After(deadline) {
				err = fmt.Errorf("shared lock: timed out while waiting for existing exclusive lock to be released")
				return
			}
		}
		time.Sleep(time.Millisecond * 500)
	}
	return
}

func (l *SharedDirLock) Lock() (err error) {
	if err = l.lockfile.Lock(); err != nil {
		return
	}
	defer func() {
		if err != nil {
			l.unlock()
		}
	}()

	// Wait until no shared lock acquired or timeout
	deadline := time.Now().Add(l.timeout)
	for {
		empty := false
		empty, err = l.assertNoSharedLocks()
		if err != nil {
			return
		}
		if empty {
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("exclusive lock: timed out while waiting for existing shared locks to be released")
		}
		time.Sleep(time.Millisecond * 500)
	}
	return
}

func (l *SharedDirLock) Unlock() (err error) {
	if err = l.lockfile.Unlock(); err != nil {
		err = fmt.Errorf("unlock: %s", err)
	}
	return
}

func (l *SharedDirLock) Close() (err error) {
	if err = os.Remove(l.sharedLockFile); err != nil {
		err = fmt.Errorf("unlock shared: %s", err)
	}
	return
}

func (l *SharedDirLock) unlock() {
	if e := l.Unlock(); e != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", e)
	}
}

func (l *SharedDirLock) assertNoSharedLocks() (r bool, err error) {
	fl, err := ioutil.ReadDir(l.dir)
	if err != nil {
		return
	}
	for _, f := range fl {
		if f.IsDir() {
			continue
		}
		ns := strings.SplitN(f.Name(), "-", 3)
		if len(ns) != 3 {
			continue
		}
		pid, e := strconv.Atoi(ns[1])
		if e != nil || pid < 1 || pid == os.Getpid() {
			continue
		}
		p, e := os.FindProcess(pid)
		if e != nil {
			// Ignore not existing process
			continue
		} else if e = p.Signal(syscall.Signal(0)); e != nil { // must check since on unix process is always returned
			// Ignore not existing process
			continue
		}
		return false, nil
	}
	return true, nil
}
