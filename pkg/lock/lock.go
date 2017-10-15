package store

type Locker interface {
	Lock() error
	Unlock() error
}

type SharedLock interface {
	Locker
	Close() error
}
