package store

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"time"

	"github.com/mgoltzsche/ctnr/bundle"
	exterrors "github.com/mgoltzsche/ctnr/pkg/errors"
	"github.com/mgoltzsche/ctnr/pkg/log"
	"github.com/pkg/errors"
)

var _ bundle.BundleStore = &BundleStore{}

type BundleStore struct {
	dir   string
	debug log.FieldLogger
	info  log.FieldLogger
}

func NewBundleStore(dir string, info log.FieldLogger, debug log.FieldLogger) *BundleStore {
	return &BundleStore{dir, debug, info}
}

func (s *BundleStore) Bundles() (l []bundle.Bundle, err error) {
	fl, e := ioutil.ReadDir(s.dir)
	l = make([]bundle.Bundle, 0, len(fl))
	if e != nil && !os.IsNotExist(e) {
		return l, errors.Wrap(err, "bundles")
	}
	for _, f := range fl {
		if f.IsDir() {
			c, e := s.Bundle(f.Name())
			if e == nil {
				l = append(l, c)
			} else {
				err = exterrors.Append(err, e)
			}
		}
	}
	return
}

func (s *BundleStore) Bundle(id string) (r bundle.Bundle, err error) {
	return bundle.NewBundle(filepath.Join(s.dir, id))
}

func (s *BundleStore) CreateBundle(builder *bundle.BundleBuilder, update bool) (b *bundle.LockedBundle, err error) {
	return builder.Build(filepath.Join(s.dir, builder.GetID()), update)
}

// Deletes all bundles that have not been used longer than the given TTL.
func (s *BundleStore) BundleGC(ttl time.Duration) (r []bundle.Bundle, err error) {
	s.debug.Printf("Running bundle GC with TTL of %s", ttl)
	before := time.Now().Add(-ttl)
	l, err := s.Bundles()
	r = make([]bundle.Bundle, 0, len(l))
	for _, b := range l {
		gcd, e := b.GC(before)
		if e != nil {
			if gcd {
				s.debug.WithField("id", b.ID()).Println("bundle gc:", e)
			}
		} else if gcd {
			s.debug.WithField("id", b.ID()).Printf("bundle garbage collected")
			r = append(r, b)
		}
	}
	return
}
