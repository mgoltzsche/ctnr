package bundle

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"time"

	"github.com/mgoltzsche/cntnr/pkg/atomic"
	lock "github.com/mgoltzsche/cntnr/pkg/lock"
	digest "github.com/opencontainers/go-digest"
	rspecs "github.com/opencontainers/runtime-spec/specs-go"
)

const (
	ANNOTATION_BUNDLE_IMAGE = "com.github.mgoltzsche.cntnr.bundle.image"
)

type Bundle struct {
	id      string
	dir     string
	created time.Time
}

func NewBundle(id, dir string, created time.Time) Bundle {
	return Bundle{id, dir, created}
}

func (b *Bundle) ID() string {
	return b.id
}

func (b *Bundle) Dir() string {
	return b.dir
}

func (b *Bundle) Created() time.Time {
	return b.created
}

func (b *Bundle) loadSpec() (r rspecs.Spec, err error) {
	file := filepath.Join(b.dir, "config.json")
	c, err := ioutil.ReadFile(file)
	if err != nil {
		return r, fmt.Errorf("bundle %q spec: %s", b.id, err)
	}
	if err = json.Unmarshal(c, &r); err != nil {
		err = fmt.Errorf("bundle %q spec: %s", b.id, err)
	}
	return
}

func (b *Bundle) Image() *digest.Digest {
	if s, err := b.loadSpec(); err == nil {
		return imageDigest(&s)
	}
	return nil
}

func imageDigest(spec *rspecs.Spec) *digest.Digest {
	if spec.Annotations != nil {
		if id := spec.Annotations[ANNOTATION_BUNDLE_IMAGE]; id != "" {
			if d, err := digest.Parse(id); err == nil {
				return &d
			}
		}
	}
	return nil
}

func (b *Bundle) Lock() (*LockedBundle, error) {
	return NewLockedBundle(*b)
}

// Update mod time so that gc doesn't touch it for a while
func (b *Bundle) resetExpiryTime() error {
	configFile := filepath.Join(b.dir)
	now := time.Now()
	os.Chtimes(configFile, now, now)
	return nil
}

func (b *Bundle) GC(before time.Time) (bool, error) {
	st, err := os.Stat(b.dir)
	if err != nil {
		return false, fmt.Errorf("bundle gc check: %s", err)
	}
	if st.ModTime().Before(before) {
		bl, err := lockBundle(b.dir)
		if err != nil {
			return false, fmt.Errorf("bundle gc check: %s", err)
		}
		defer func() {
			if err := bl.Unlock(); err != nil {
				fmt.Fprintf(os.Stderr, "bundle gc check: %s\n", err)
			}
		}()
		st, err = os.Stat(b.dir)
		if err != nil {
			return true, fmt.Errorf("bundle gc check: %s", err)
		}
		if st.ModTime().Before(before) {
			if err = deleteBundle(b.dir); err != nil {
				return true, fmt.Errorf("garbage collect: %s", err)
			}
		} else {
			return false, err
		}
		return true, err
	} else {
		return false, nil
	}
}

type LockedBundle struct {
	Bundle
	spec rspecs.Spec
	lock *lock.Lockfile
}

func NewLockedBundle(bundle Bundle) (*LockedBundle, error) {
	lck, err := lockBundle(bundle.dir)
	if err := bundle.resetExpiryTime(); err != nil {
		return nil, fmt.Errorf("lock bundle: %s", err)
	}
	spec, err := bundle.loadSpec()
	if err != nil {
		return nil, fmt.Errorf("lock bundle: %s", err)
	}
	return &LockedBundle{bundle, spec, lck}, err
}

func (b *LockedBundle) Close() (err error) {
	if b.lock != nil {
		if err = b.resetExpiryTime(); err != nil {
			fmt.Fprintf(os.Stderr, "unlock bundle: %s", err)
			err = fmt.Errorf("unlock bundle: %s", err)
		}
		if e := b.lock.Unlock(); e != nil && err == nil {
			if err == nil {
				err = fmt.Errorf("unlock bundle: %s", e)
			} else {
				err = fmt.Errorf("unlock bundle: %s. %s", err, e)
			}

		}
		b.lock = nil
	}
	return
}

func (b *LockedBundle) Image() *digest.Digest {
	return imageDigest(&b.spec)
}

func (b *LockedBundle) SetParentImageId(imageID string) (err error) {
	b.spec.Annotations[ANNOTATION_BUNDLE_IMAGE] = imageID
	if _, err = atomic.WriteJson(filepath.Join(b.dir, "config.json"), &b.spec); err != nil {
		err = fmt.Errorf("set bundle's parent image id: %s", err)
	}
	return
}

func (b *LockedBundle) Delete() (err error) {
	err = deleteBundle(b.dir)
	if e := b.Close(); e != nil {
		if err == nil {
			err = fmt.Errorf("delete bundle: %s", e)
		} else {
			err = fmt.Errorf("delete bundle: %s. %s", err, e)
		}
	}
	return
}

func lockBundle(dir string) (l *lock.Lockfile, err error) {
	// TODO: use tmpfs for lock file
	l, err = lock.LockFile(filepath.Clean(dir) + ".lock")
	if err != nil {
		return nil, fmt.Errorf("lock bundle: %s", err)
	}
	if err = l.Lock(); err != nil {
		return nil, fmt.Errorf("lock bundle: %s", err)
	}
	return l, err
}

func deleteBundle(dir string) (err error) {
	if err = os.RemoveAll(dir); err != nil {
		err = fmt.Errorf("delete bundle: %s", err)
	}
	return
}
