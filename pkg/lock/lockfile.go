package store

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/nightlyone/lockfile"
)

type Lockfile struct {
	file     string
	lockfile lockfile.Lockfile
}

func LockFile(file string) (*Lockfile, error) {
	file, err := normalize(file)
	if err != nil {
		return nil, fmt.Errorf("lock file: %s", err)
	}

	l, err := lockfile.New(file)
	lck := &Lockfile{file, l}
	return lck, err
}

func (l *Lockfile) Lock() (err error) {
	lock(l.file)

	defer func() {
		if err != nil {
			unlock(l.file)
		}
	}()

	for {
		if l.lockfile.TryLock() == nil {
			return
		}

		if err = awaitFileChange(l.file); err != nil && !os.IsNotExist(err) {
			return
		}
	}
	return
}

func (l *Lockfile) Unlock() error {
	defer unlock(l.file)
	return l.lockfile.Unlock()
}

func (l *Lockfile) IsLocked() (bool, error) {
	// TODO: verify lock and delete old lock file
	_, err := os.Stat(string(l.lockfile))
	if os.IsNotExist(err) {
		return false, nil
	} else {
		return true, err
	}
}

func normalize(path string) (f string, err error) {
	if f, err = filepath.EvalSymlinks(path); err != nil {
		if os.IsNotExist(err) {
			f, err = normalize(filepath.Dir(path))
			f = filepath.Join(f, filepath.Base(path))
		}
		if err != nil {
			return
		}
	}
	return filepath.Abs(f)
}

func awaitFileChange(files ...string) (err error) {
	if len(files) == 0 {
		panic("No files provided to watch")
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return
	}
	defer watcher.Close()
	for _, file := range files {
		if err = watcher.Add(file); err != nil {
			return
		}
	}
	timer := time.NewTimer(5 * time.Second)
	select {
	case event := <-watcher.Events:
		log.Println("watch event:", event)
		return
	case err = <-watcher.Errors:
		log.Println("watch err:", err)
		return
	case <-timer.C:
		// Timeout to prevent deadlock after other process dies without deleting its lockfile
		log.Println("watch time expired")
		return
	}
}
