package bundle

import (
	"time"
)

type BundleStore interface {
	CreateBundle(id string, update bool) (*LockedBundle, error)
	Bundle(id string) (Bundle, error)
	Bundles() ([]Bundle, error)
	BundleGC(ttl time.Duration) ([]Bundle, error)
}
