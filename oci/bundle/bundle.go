package bundle

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mgoltzsche/cntnr/pkg/atomic"
	lock "github.com/mgoltzsche/cntnr/pkg/lock"
	digest "github.com/opencontainers/go-digest"
	rspecs "github.com/opencontainers/runtime-spec/specs-go"
	gen "github.com/opencontainers/runtime-tools/generate"
)

type Bundle struct {
	id      string
	dir     string
	created time.Time
}

func NewBundle(dir string) (r Bundle, err error) {
	r.id = filepath.Base(dir)
	f, err := os.Stat(dir)
	if err != nil {
		return r, fmt.Errorf("open bundle: %s", err)
	}
	if !f.IsDir() {
		return r, fmt.Errorf("open bundle: no directory provided but %s", dir)
	}
	r.dir = dir
	r.created = f.ModTime()
	return
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
	if imgIdb, err := ioutil.ReadFile(b.imageFile()); err == nil {
		d, err := digest.Parse(strings.Trim(string(imgIdb), " \n"))
		if err == nil {
			return &d
		}
	}
	return nil
}

func (b *Bundle) imageFile() string {
	return filepath.Join(b.dir, "rootfs.image")
}

func (b *Bundle) Lock() (*LockedBundle, error) {
	return OpenLockedBundle(*b)
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
		bl, err := lockBundle(b)
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
	bundle Bundle
	spec   *rspecs.Spec
	image  *digest.Digest
	lock   *lock.Lockfile
}

func OpenLockedBundle(bundle Bundle) (*LockedBundle, error) {
	lck, err := lockBundle(&bundle)
	if err != nil {
		return nil, err
	}
	if err := bundle.resetExpiryTime(); err != nil {
		return nil, fmt.Errorf("lock bundle: %s", err)
	}
	return &LockedBundle{bundle, nil, nil, lck}, nil
}

func CreateLockedBundle(dir string, spec *gen.Generator, image BundleImage) (r *LockedBundle, err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("create bundle: %s", err)
		}
	}()

	// Create bundle
	id := ""
	sp := spec.Spec()
	if sp.Annotations != nil {
		id = sp.Annotations[ANNOTATION_BUNDLE_ID]
	}
	if id == "" {
		id = filepath.Base(dir)
	}
	bundle := Bundle{id, dir, time.Now()}

	// Create bundle directory
	if err = os.Mkdir(dir, 0770); err != nil {
		return
	}
	defer func() {
		if err != nil {
			os.RemoveAll(dir)
		}
	}()

	// Lock bundle
	lck, err := lockBundle(&bundle)
	if err != nil {
		return nil, err
	}
	r = &LockedBundle{bundle, nil, nil, lck}
	defer func() {
		if err != nil {
			r.Close()
		}
	}()

	// Update config.json and rootfs
	if err = r.SetSpec(spec); err != nil {
		return
	}
	err = r.UpdateRootfs(image)
	return
}

func (b *LockedBundle) Close() (err error) {
	if b.lock != nil {
		if err = b.bundle.resetExpiryTime(); err != nil {
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

func (b *LockedBundle) ID() string {
	return b.bundle.id
}

func (b *LockedBundle) Dir() string {
	return b.bundle.dir
}

func (b *LockedBundle) Spec() (*rspecs.Spec, error) {
	if b.spec == nil {
		spec, err := b.bundle.loadSpec()
		if err != nil {
			return nil, err
		}
		b.spec = &spec
	}
	return b.spec, nil
}

func (b *LockedBundle) UpdateRootfs(image BundleImage) (err error) {
	spec, err := b.Spec()
	if err != nil {
		return
	}
	rootfs := filepath.Join(b.Dir(), spec.Root.Path)
	if err = os.MkdirAll(rootfs, 0755); err != nil {
		return
	}
	if image == nil {
		err = b.SetParentImageId(nil)
	} else {
		// Unpack image
		oldImageId := b.Image()
		if oldImageId == nil || *oldImageId != image.ID() {
			// Update rootfs when image changed only
			if err = image.Unpack(rootfs); err != nil {
				return
			}
			imageId := image.ID()
			err = b.SetParentImageId(&imageId)
		}
	}
	return
}

func (b *LockedBundle) SetSpec(spec *gen.Generator) (err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("update bundle %q spec: %s", b.ID(), err)
		}
	}()

	// Create volume directories
	if mounts := spec.Spec().Mounts; mounts != nil {
		for _, mount := range mounts {
			if mount.Type == "bind" {
				src := mount.Source
				if !filepath.IsAbs(src) {
					src = filepath.Join(b.Dir(), src)
				}
				if _, err = os.Stat(src); os.IsNotExist(err) {
					if err = os.MkdirAll(src, 0755); err != nil {
						return
					}
				}
			}
		}
	}

	// Write config.json
	spec.AddAnnotation(ANNOTATION_BUNDLE_ID, b.ID())
	tmpConfFile, err := ioutil.TempFile(b.Dir(), "tmp-conf-")
	if err != nil {
		return
	}
	tmpConfPath := tmpConfFile.Name()
	tmpConfRemoved := false
	defer func() {
		tmpConfFile.Close()
		if !tmpConfRemoved {
			os.Remove(tmpConfPath)
		}
	}()
	if err = spec.Save(tmpConfFile, gen.ExportOptions{Seccomp: false}); err != nil {
		return
	}
	tmpConfFile.Close()
	confFile := filepath.Join(b.Dir(), "config.json")
	if err = os.Rename(tmpConfPath, confFile); err != nil {
		return
	}
	tmpConfRemoved = true
	b.spec = spec.Spec()
	return
}

// Reads image ID from cached spec
func (b *LockedBundle) Image() *digest.Digest {
	if b.image == nil {
		b.image = b.bundle.Image()
	}
	return b.image
}

func (b *LockedBundle) SetParentImageId(imageID *digest.Digest) error {
	if imageID == nil {
		if e := os.Remove(b.bundle.imageFile()); e != nil && !os.IsNotExist(e) {
			return fmt.Errorf("set bundle's parent image id: %s", e)
		}
	} else if _, err := atomic.WriteFile(b.bundle.imageFile(), bytes.NewBufferString((*imageID).String())); err != nil {
		return fmt.Errorf("set bundle's parent image id: %s", err)
	}
	b.image = imageID
	return nil
}

func (b *LockedBundle) Delete() (err error) {
	err = deleteBundle(b.Dir())
	if e := b.Close(); e != nil {
		if err == nil {
			err = fmt.Errorf("delete bundle: %s", e)
		} else {
			err = fmt.Errorf("delete bundle: %s. %s", err, e)
		}
	}
	return
}

func lockBundle(bundle *Bundle) (l *lock.Lockfile, err error) {
	// TODO: use tmpfs for lock file
	l, err = lock.LockFile(filepath.Clean(bundle.dir) + ".lock")
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
