package lock

type Locker interface {
	Lock() error
	Unlocker
}

type Unlocker interface {
	Unlock() error
}

type ExclusiveLocker interface {
	Locker
	NewSharedLocker() Locker
}
