package bundle

import (
	"time"
)

type BundleStore interface {
	CreateBundle(id string, builder *BundleBuilder) (Bundle, error)
	Bundle(id string) (Bundle, error)
	Bundles() ([]Bundle, error)
	BundleGC(before time.Time) ([]Bundle, error)
}
