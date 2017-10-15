package store

import (
	"sync"
)

var (
	locks = map[string]*kvmutex{}
	mutex = &sync.Mutex{}
)

type kvmutex struct {
	mutex sync.Mutex
	count uint
}

func lock(key string) {
	l := mutexRef(key)
	l.mutex.Lock()
}

func mutexRef(key string) *kvmutex {
	mutex.Lock()
	defer mutex.Unlock()

	l := locks[key]
	if l == nil {
		l = &kvmutex{}
		locks[key] = l
	}
	l.count++
	return l
}

func unlock(key string) {
	mutex.Lock()
	defer mutex.Unlock()

	l := locks[key]
	if l == nil {
		panic("unlock: mutex unlocked: " + key)
	}
	l.mutex.Unlock()
	l.count--
	if l.count == 0 {
		delete(locks, key)
	}
}
