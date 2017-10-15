package store

import (
	"fmt"
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

type ImageWriter interface {
	CommitImage(rootfs, name string, parentManifest *digest.Digest, author, comment string) (Image, error)
}

type Bundle struct {
	ID      string
	Dir     string
	Spec    rspecs.Spec
	Created time.Time
}

func (b *Bundle) Image() *digest.Digest {
	if id := b.Spec.Annotations[ANNOTATION_BUNDLE_IMAGE]; id != "" {
		if d, err := digest.Parse(id); err == nil {
			return &d
		}
	}
	return nil
}

func (b *Bundle) Lock() (*LockedBundle, error) {
	return NewLockedBundle(*b)
}

// Updates config.json mod time so that gc doesn't touch it for a while
func (b *Bundle) resetExpiryTime() error {
	configFile := filepath.Join(b.Dir)
	now := time.Now()
	os.Chtimes(configFile, now, now)
	return nil
}

func (b *Bundle) GC(before time.Time) (bool, error) {
	st, err := os.Stat(b.Dir)
	if err != nil {
		return false, fmt.Errorf("bundle gc check: %s", err)
	}
	if st.ModTime().Before(before) {
		bl, err := lockBundle(b.Dir)
		if err != nil {
			return false, fmt.Errorf("bundle gc check: %s", err)
		}
		defer func() {
			if err := bl.Unlock(); err != nil {
				fmt.Fprintf(os.Stderr, "bundle gc check: %s\n", err)
			}
		}()
		st, err = os.Stat(b.Dir)
		if err != nil {
			return true, fmt.Errorf("bundle gc check: %s", err)
		}
		if st.ModTime().Before(before) {
			if err = deleteBundle(b.Dir); err != nil {
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
	lock *lock.Lockfile
}

func NewLockedBundle(bundle Bundle) (*LockedBundle, error) {
	lck, err := lockBundle(bundle.Dir)
	if err := bundle.resetExpiryTime(); err != nil {
		return nil, fmt.Errorf("lock bundle: %s", err)
	}
	return &LockedBundle{bundle, lck}, err
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

func (b *LockedBundle) Commit(writer ImageWriter, name, author, comment string) (r Image, err error) {
	// Commit layer
	rootfs := filepath.Join(b.Dir, "rootfs")
	r, err = writer.CommitImage(rootfs, name, b.Image(), author, comment)
	if err != nil {
		return
	}

	// Update container parent
	b.Spec.Annotations[ANNOTATION_BUNDLE_IMAGE] = r.ID.String()
	if _, err = atomic.WriteJson(filepath.Join(rootfs, "config.json"), &b.Spec); err != nil {
		err = fmt.Errorf("commit: %s", err)
	}
	return
}

func (b *LockedBundle) Delete() (err error) {
	err = deleteBundle(b.Dir)
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
