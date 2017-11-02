package lock

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// TODO: Make sure sharedLockDir is thread-safe.
//   Currently this is not the case since Lock() can be called concurrently while changing the sharedLockFile property this results in an inconsistent state
//     => already fixed by having sharedDirLock acquire lock at construction time again

type sharedDirLock struct {
	lockfile       *Lockfile
	sharedLockFile string
	dir            string
}

func NewSharedLock(dir string) (r SharedLock, err error) {
	if err = os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("init lock directory: %s", err)
	}
	l := sharedDirLock{}
	if l.lockfile, err = LockFile(filepath.Join(dir, "exclusive.lock")); err != nil {
		return
	}
	l.dir = dir
	l.sharedLockFile, err = l.lockShared()
	return &l, err
}

func (l *sharedDirLock) lockShared() (f string, err error) {
	// Lock dir exclusively
	err = l.lockfile.Lock()
	if err != nil {
		err = fmt.Errorf("shared lock: %s", err)
		return
	}
	defer l.lockfile.Unlock()

	// Register shared lock file
	file, err := ioutil.TempFile(l.dir, fmt.Sprintf("sharedlock-%d-", os.Getpid()))
	if err != nil {
		err = fmt.Errorf("shared lock: %s", err)
		return
	}
	f = file.Name()
	file.Close()
	return
}

func (l *sharedDirLock) Close() (err error) {
	if l.sharedLockFile == "" {
		// If this happens there is some serious misusage of this package
		// happening which can lead to further errors due to inconsistency.
		panic("unlock: invalid state - was not locked")
	}
	if err = os.Remove(l.sharedLockFile); err != nil {
		err = fmt.Errorf("unlock shared: %s", err)
	}
	l.sharedLockFile = ""
	return
}

func (l *sharedDirLock) Lock() (err error) {
	if err = l.lockfile.Lock(); err != nil {
		return
	}
	defer func() {
		if err != nil {
			if e := l.Unlock(); e != nil {
				err = fmt.Errorf("%s, lock: %s", err, e)
			}
		}
	}()

	// Wait until no shared lock acquired
	if err = l.awaitSharedLocks(); err != nil {
		err = fmt.Errorf("lock: await free shared lock: %s", err)
	}
	return
}

func (l *sharedDirLock) Unlock() (err error) {
	if err = l.lockfile.Unlock(); err != nil {
		err = fmt.Errorf("unlock: %s", err)
	}
	return
}

func (l *sharedDirLock) awaitSharedLocks() (err error) {
	ownFile := filepath.Base(l.sharedLockFile)
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
			if e != nil || pid < 1 || fname == ownFile {
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
				err = fmt.Errorf("await exclusive usage: %s", e)
				return
			}
			break
		}
	}
	return
}
