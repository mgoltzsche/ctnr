package store

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/nightlyone/lockfile"
)

var (
	locks = map[string]*Lockfile{}
	mutex = &sync.Mutex{}
)

type Lockfile struct {
	file     string
	lockfile lockfile.Lockfile
	timeout  time.Duration
	mutex    *sync.Mutex
}

func LockFile(file string, retryTimeout time.Duration) (*Lockfile, error) {
	mutex.Lock()
	defer mutex.Unlock()

	file, err := normalize(file)
	if err != nil {
		return nil, fmt.Errorf("lock file: %s", err)
	}

	if exist := locks[file]; exist != nil {
		return exist, nil
	}

	l, err := lockfile.New(file)
	lck := &Lockfile{file, l, retryTimeout, &sync.Mutex{}}
	locks[file] = lck
	return lck, err
}

func (l *Lockfile) Lock() error {
	l.mutex.Lock()
	maxRetries := l.timeout.Seconds()
	var n float64
	for {
		if l.lockfile.TryLock() == nil {
			return nil
		}

		// TODO: remove (check calls first)
		n++
		if n > maxRetries {
			return fmt.Errorf("lock %s: timed out", string(l.lockfile))
		}

		if err := awaitFileChange(l.file); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func (l *Lockfile) Unlock() error {
	defer l.mutex.Unlock()
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

func (l *Lockfile) Close() error {
	mutex.Lock()
	defer mutex.Unlock()

	delete(locks, l.file)
	l.mutex = nil
	return nil
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

func awaitFileChange(file string) (err error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return
	}
	defer watcher.Close()

	if err = watcher.Add(file); err != nil {
		return
	}

	select {
	case event := <-watcher.Events:
		log.Println("watch event:", event)
		return
	case err = <-watcher.Errors:
		log.Println("watch err:", err)
		return
	}
}
