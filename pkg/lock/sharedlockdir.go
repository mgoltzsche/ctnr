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

func NewSharedLock(dir string) (l SharedDirLock, err error) {
	if err = os.MkdirAll(dir, 0755); err != nil {
		err = fmt.Errorf("init lock directory: %s", err)
		return
	}
	l.dir = dir

	if l.lockfile, err = LockFile(filepath.Join(dir, "exclusive.lock")); err != nil {
		return
	}

	// Lock dir exclusively
	err = l.lockfile.Lock()
	if err != nil {
		err = fmt.Errorf("shared lock: %s", err)
		return
	}
	defer l.lockfile.Unlock()

	// Register shared lock file
	tmp, e := ioutil.TempFile(dir, fmt.Sprintf("sharedlock-%d-", os.Getpid()))
	if e != nil {
		err = fmt.Errorf("shared lock: %s", e)
		return
	}
	l.sharedLockFile = tmp.Name()
	tmp.Close()
	return
}

func (l *SharedDirLock) Lock() (err error) {
	if err = l.lockfile.Lock(); err != nil {
		return
	}
	defer func() {
		if err != nil {

		}
	}()

	// Wait until no shared lock acquired
	err = l.awaitSharedLocks()
	if err != nil {
		if e := l.Unlock(); e != nil {
			err = fmt.Errorf("lock: await free lock: %s, %s", err, e)
		} else {
			err = fmt.Errorf("lock: await free lock: %s", err)
		}
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

func (l *SharedDirLock) awaitSharedLocks() (err error) {
	own := filepath.Base(l.sharedLockFile)
	locked := true
	var fl []os.FileInfo
	for locked {
		fl, err = ioutil.ReadDir(l.dir)
		if err != nil {
			return
		}
		locked = false
		for _, f := range fl {
			if f.IsDir() {
				continue
			}
			fname := f.Name()
			fpath := filepath.Join(l.dir, fname)
			ns := strings.SplitN(fname, "-", 3)
			if len(ns) != 3 || ns[2] == "" {
				continue
			}
			pid, e := strconv.Atoi(ns[1])
			if e != nil || pid < 1 || fname == own {
				// ignore non shared lock files + own lock file
				continue
			}
			p, e := os.FindProcess(pid)
			if e != nil || p.Signal(syscall.Signal(0)) != nil {
				// Ignore and remove file from not existing process
				//TODO: os.Remove(fpath)
				continue
			}
			locked = true
			if e = awaitFileChange(fpath, l.dir); e != nil && !os.IsNotExist(e) {
				err = fmt.Errorf("lock: await exclusive usage: %s", e)
				return
			}
			break
		}
	}
	return
}
