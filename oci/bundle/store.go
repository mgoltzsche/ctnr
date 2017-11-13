package bundle

import (
	"time"
)

type BundleStore interface {
	CreateBundle(builder *BundleBuilder) (Bundle, error)
	Bundle(id string) (Bundle, error)
	Bundles() ([]Bundle, error)
	BundleGC(before time.Time) ([]Bundle, error)
}
