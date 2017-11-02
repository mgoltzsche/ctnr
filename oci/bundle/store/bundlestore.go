package store

import (
	"encoding/base32"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mgoltzsche/cntnr/log"
	"github.com/mgoltzsche/cntnr/oci/bundle"
	"github.com/satori/go.uuid"
)

var _ bundle.BundleStore = &BundleStore{}

type BundleStore struct {
	dir   string
	debug log.Logger
}

func NewBundleStore(dir string, debugLog log.Logger) (s *BundleStore, err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("init bundle store: %s", err)
		}
	}()
	if dir, err = filepath.Abs(dir); err != nil {
		return
	}
	if err = os.MkdirAll(dir, 0755); err != nil {
		return
	}
	return &BundleStore{dir, debugLog}, err
}

func (s *BundleStore) Bundles() (l []bundle.Bundle, err error) {
	fl, err := ioutil.ReadDir(s.dir)
	l = make([]bundle.Bundle, 0, len(fl))
	if err != nil {
		return l, fmt.Errorf("bundles: %s", err)
	}
	for _, f := range fl {
		if f.IsDir() {
			c, e := s.Bundle(f.Name())
			if e == nil {
				l = append(l, c)
			} else {
				s.debug.Printf("bundles: %s", e)
				err = e
			}
		}
	}
	return
}

func (s *BundleStore) Bundle(id string) (r bundle.Bundle, err error) {
	dir := filepath.Join(s.dir, id)
	f, err := os.Stat(dir)
	if err != nil {
		return r, fmt.Errorf("bundle %q not found: %s", id, err)
	}
	return bundle.NewBundle(id, dir, f.ModTime()), nil
}

func (s *BundleStore) CreateBundle(id string, builder *bundle.BundleBuilder) (bundle.Bundle, error) {
	if id == "" {
		id = generateId()
	}
	return builder.Build(filepath.Join(s.dir, id))
}

// Deletes all bundles older than the given time
func (s *BundleStore) BundleGC(before time.Time) (r []bundle.Bundle, err error) {
	l, err := s.Bundles()
	r = make([]bundle.Bundle, 0, len(l))
	if err != nil {
		s.debug.Printf("bundle gc: %s", err)
	}
	for _, b := range l {
		gcd, e := b.GC(before)
		if e != nil {
			s.debug.Printf("bundle gc: %s", e)
			if gcd {
				err = e
			}
		} else if gcd {
			r = append(r, b)
		}
	}
	return
}

// TODO: Move into utils package since it also occurs in run
func generateId() string {
	return strings.ToLower(strings.TrimRight(base32.StdEncoding.EncodeToString(uuid.NewV4().Bytes()), "="))
}
