package lock

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/pkg/errors"
)

// TODO: Make sure sharedLockDir is thread-safe.
//   Currently this is not the case since Lock() can be called concurrently while changing the sharedLockFile property this results in an inconsistent state
//     => already fixed by having sharedDirLock acquire lock at construction time again

type exclusiveLocker struct {
	lockfile *Lockfile
	dir      string
}

/*type noopLocker string

func NewNoopLocker() Locker {
	return noopLocker("nooplocker")
}

func (l *NoopLocker) Locker {

}*/

func NewExclusiveDirLocker(dir string) (r ExclusiveLocker, err error) {
	if err = os.MkdirAll(dir, 0755); err != nil {
		return nil, errors.Wrap(err, "init lock directory")
	}
	l := exclusiveLocker{}
	if l.lockfile, err = LockFile(filepath.Join(dir, "exclusive.lock")); err != nil {
		return
	}
	l.dir = dir
	return &l, err
}

func (l *exclusiveLocker) NewSharedLocker() Locker {
	return &sharedLocker{"", l.dir, l.lockfile}
}

func (l *exclusiveLocker) Lock() (err error) {
	if err = l.lockfile.Lock(); err != nil {
		return
	}
	defer func() {
		if err != nil {
			if e := l.Unlock(); e != nil {
				// TODO: split error properly
				err = errors.Errorf("%s, lock: %s", err, e)
			}
		}
	}()

	// Wait until no shared lock acquired
	return errors.Wrap(l.awaitSharedLocks(), "lock")
}

func (l *exclusiveLocker) Unlock() error {
	return errors.Wrap(l.lockfile.Unlock(), "unlock")
}

func (l *exclusiveLocker) awaitSharedLocks() (err error) {
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
			if e != nil || pid < 1 {
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
				err = errors.Wrap(e, "await exclusive lock usage")
				return
			}
			break
		}
	}
	return
}

type sharedLocker struct {
	sharedLockFile string
	dir            string
	exclusive      *Lockfile
}

func (l *sharedLocker) Lock() (err error) {
	if l.sharedLockFile != "" {
		panic("lock shared: shared lock is already locked: " + l.sharedLockFile)
	}

	// Lock dir exclusively
	err = l.exclusive.Lock()
	if err != nil {
		err = errors.Wrap(err, "shared lock")
		return
	}
	defer l.exclusive.Unlock()

	// Register shared lock file
	file, err := ioutil.TempFile(l.dir, fmt.Sprintf("sharedlock-%d-", os.Getpid()))
	if err != nil {
		err = errors.Wrap(err, "shared lock")
		return
	}
	l.sharedLockFile = file.Name()
	file.Close()
	return
}

func (l *sharedLocker) Unlock() (err error) {
	if l.sharedLockFile == "" {
		// If this happens there is some serious misusage of this package
		// happening which can lead to further errors due to inconsistency.
		panic("unlock shared: invalid state - was not locked")
	}
	if err = os.Remove(l.sharedLockFile); err != nil {
		err = errors.Wrap(err, "unlock shared")
	}
	l.sharedLockFile = ""
	return
}
