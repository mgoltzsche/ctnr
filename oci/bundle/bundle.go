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

	"github.com/hashicorp/go-multierror"
	"github.com/mgoltzsche/cntnr/pkg/atomic"
	lock "github.com/mgoltzsche/cntnr/pkg/lock"
	digest "github.com/opencontainers/go-digest"
	rspecs "github.com/opencontainers/runtime-spec/specs-go"
	gen "github.com/opencontainers/runtime-tools/generate"
	"github.com/pkg/errors"
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

func (b *Bundle) GC(before time.Time) (r bool, err error) {
	st, err := os.Stat(b.dir)
	if err != nil {
		return false, fmt.Errorf("bundle gc check: %s", err)
	}
	if st.ModTime().Before(before) {
		var bl *lock.Lockfile
		bl, err = lockBundle(b)
		if err != nil {
			return false, fmt.Errorf("bundle gc check: %s", err)
		}
		defer func() {
			if e := bl.Unlock(); e != nil {
				err = multierror.Append(err, fmt.Errorf("bundle gc check: %s", err))
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

func CreateLockedBundle(dir string, spec *gen.Generator, image BundleImage, update bool) (r *LockedBundle, err error) {
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

	// Create or update bundle
	_, e := os.Stat(dir)
	exists := !os.IsNotExist(e)

	if exists {
		if !update {
			return r, fmt.Errorf("bundle directory %s already exists", dir)
		}
		lastImageId := bundle.Image()
		if !(lastImageId == nil && image == nil || lastImageId != nil && *lastImageId == image.ID()) {
			// Update rootfs only if changed
			if err = r.UpdateRootfs(image); err != nil {
				return
			}
		}
		if err = r.SetSpec(spec); err != nil {
			return
		}
	} else {
		// Create bundle directory
		if err = os.Mkdir(dir, 0770); err != nil {
			return
		}

		defer func() {
			if err != nil {
				err = multierror.Append(err, os.RemoveAll(dir))
			}
		}()

		// Write config.json and rootfs
		if err = r.UpdateRootfs(image); err != nil {
			return
		}
		if err = r.SetSpec(spec); err != nil {
			return
		}
	}

	return
}

func (b *LockedBundle) Close() (err error) {
	if b.lock != nil {
		err = b.bundle.resetExpiryTime()
		if e := b.lock.Unlock(); e != nil {
			err = multierror.Append(err, e)
		}
		if err != nil {
			err = errors.Wrap(err, "unlock bundle")
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
	rootfs := filepath.Join(b.Dir(), "rootfs")
	var imgId *digest.Digest
	if image != nil {
		id := image.ID()
		imgId = &id
	}
	if err = createRootfs(rootfs, image); err != nil {
		return
	}
	return b.SetParentImageId(imgId)
}

func createRootfs(rootfs string, image BundleImage) (err error) {
	if e := os.RemoveAll(rootfs); e != nil && os.IsNotExist(e) {
		return e
	}
	if err = os.Mkdir(rootfs, 0755); err != nil {
		return
	}
	if image != nil {
		// Unpack image
		if err = image.Unpack(rootfs); err != nil {
			return
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

	if err = createVolumeDirectories(spec.Spec(), b.Dir()); err != nil {
		return
	}

	// Write config.json
	if spec.Spec().Root != nil {
		spec.Spec().Root.Path = "rootfs"
	}
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

func createVolumeDirectories(spec *rspecs.Spec, dir string) (err error) {
	if spec != nil && spec.Mounts != nil {
		for _, mount := range spec.Mounts {
			if mount.Type == "bind" {
				src := mount.Source
				if !filepath.IsAbs(src) {
					src = filepath.Join(dir, src)
				}
				if _, err = os.Stat(src); os.IsNotExist(err) {
					if err = os.MkdirAll(src, 0755); err != nil {
						break
					}
				}
			}
		}
	}
	if err != nil {
		err = fmt.Errorf("volume directories from spec: %s", err)
	}
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
		err = multierror.Append(err, e)
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
