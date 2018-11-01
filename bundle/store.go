package bundle

import (
	"time"
)

type BundleStore interface {
	CreateBundle(id string, update bool) (*LockedBundle, error)
	Bundle(id string) (Bundle, error)
	Bundles() ([]Bundle, error)
	BundleGC(ttl time.Duration, containers ContainerStore) ([]Bundle, error)
}

type ContainerStore interface {
	Exist(id string) (bool, error)
}
