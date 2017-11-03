package lock

type Locker interface {
	Lock() error
	Unlock() error
}

type ExclusiveLocker interface {
	Locker
	NewSharedLocker() Locker
}
