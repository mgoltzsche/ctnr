package store

type Locker interface {
	Lock() error
	Unlock() error
}

/*type SharedLocker interface {
	SharedLock() (Lock, error)
}*/

type Lock interface {
	Unlock() error
}

type SharedLock interface {
	Lock() error
	Unlock() error
	Close() error
}
