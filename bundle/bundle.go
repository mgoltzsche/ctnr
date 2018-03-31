package bundle

import (
	"bytes"
	"encoding/json"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hashicorp/go-multierror"
	"github.com/mgoltzsche/cntnr/pkg/atomic"
	exterrors "github.com/mgoltzsche/cntnr/pkg/errors"
	"github.com/mgoltzsche/cntnr/pkg/lock"
	digest "github.com/opencontainers/go-digest"
	rspecs "github.com/opencontainers/runtime-spec/specs-go"
	gen "github.com/opencontainers/runtime-tools/generate"
	"github.com/pkg/errors"
)

const ANNOTATION_BUNDLE_ID = "com.github.mgoltzsche.cntnr.bundle.id"

type Bundle struct {
	id      string
	dir     string
	created time.Time
}

func NewBundle(dir string) (r Bundle, err error) {
	r.id = filepath.Base(dir)
	f, err := os.Stat(dir)
	if err != nil {
		return r, errors.Wrap(err, "open bundle")
	}
	if !f.IsDir() {
		return r, errors.Errorf("open bundle: no directory provided but %s", dir)
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
	if err == nil {
		err = json.Unmarshal(c, &r)
	}
	if err != nil {
		err = errors.Errorf("bundle %q spec: %s", b.id, err)
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
	defer exterrors.Wrapd(&err, "bundle gc check")
	st, err := os.Stat(b.dir)
	if err != nil {
		return false, errors.New(err.Error())
	}
	if st.ModTime().Before(before) {
		var bl *lock.Lockfile
		bl, err = lockBundle(b)
		if err != nil {
			return true, err
		}
		defer func() {
			if e := bl.Unlock(); e != nil {
				err = multierror.Append(err, e)
			}
		}()
		if st, err = os.Stat(b.dir); err != nil {
			return true, err
		}
		if st.ModTime().Before(before) {
			if err = deleteBundle(b.dir); err != nil {
				return true, err
			}
			return true, err
		}
	}
	return
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
		return nil, errors.Wrap(err, "lock bundle")
	}
	return &LockedBundle{bundle, nil, nil, lck}, nil
}

func CreateLockedBundle(dir string, spec *gen.Generator, image BundleImage, update bool) (r *LockedBundle, err error) {
	defer exterrors.Wrapd(&err, "create bundle")

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
	r = &LockedBundle{bundle, spec.Spec(), nil, lck}
	defer func() {
		if err != nil {
			if e := r.Close(); e != nil {
				err = multierror.Append(err, e)
			}
		}
	}()

	// Create or update bundle
	err = os.Mkdir(dir, 0770)
	exists := err != nil && os.IsExist(err)
	updateRootfs := true

	if exists {
		if !update {
			return nil, errors.Errorf("bundle %q directory already exists", id)
		}
		lastImageId := bundle.Image()
		rootfs := filepath.Join(dir, spec.Spec().Root.Path)
		if _, e := os.Stat(rootfs); e == nil && (lastImageId == nil && image == nil || lastImageId != nil && *lastImageId == image.ID()) {
			updateRootfs = false
		}
	} else {
		if err != nil {
			return
		}
		if !update {
			defer func() {
				if err != nil {
					err = multierror.Append(err, os.RemoveAll(dir))
				}
			}()
		}
	}

	// Update rootfs if not exists or image changed
	if updateRootfs {
		if err = r.UpdateRootfs(image); err != nil {
			return
		}
	}

	// Write spec
	if err = r.SetSpec(spec); err != nil {
		return
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
	spec, err := b.Spec()
	if err != nil {
		return
	}
	rootfs := filepath.Join(b.Dir(), spec.Root.Path)
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
	if err = os.RemoveAll(rootfs); err == nil || os.IsNotExist(err) {
		if err = os.Mkdir(rootfs, 0755); err == nil && image != nil {
			return image.Unpack(rootfs)
		}
	}
	if err != nil {
		err = errors.New("create bundle rootfs: " + err.Error())
	}
	return
}

func (b *LockedBundle) SetSpec(spec *gen.Generator) (err error) {
	defer exterrors.Wrapdf(&err, "update bundle %q spec", b.ID())

	/*if err = createVolumeDirectories(spec.Spec(), b.Dir()); err != nil {
		return
	}*/

	// Write config.json
	if spec.Spec().Root != nil {
		spec.Spec().Root.Path = "rootfs"
	}
	spec.AddAnnotation(ANNOTATION_BUNDLE_ID, b.ID())
	tmpConfFile, err := ioutil.TempFile(b.Dir(), ".tmp-conf-")
	if err != nil {
		return errors.New(err.Error())
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
		return errors.New(err.Error())
	}
	tmpConfRemoved = true
	b.spec = spec.Spec()
	return
}

/*func createVolumeDirectories(spec *rspecs.Spec, dir string) (err error) {
	if spec != nil && spec.Mounts != nil {
		for _, mount := range spec.Mounts {
			if mount.Type == "bind" {
				src := mount.Source
				if !filepath.IsAbs(src) {
					src = filepath.Join(dir, src)
				}
				relsrc := filepath.Clean(mount.Source)
				if _, err = os.Stat(src); os.IsNotExist(err) && !filepath.IsAbs(relsrc) && strings.Index(relsrc, "..") != 0 {
					err = errors.Errorf("bind mount source %q does not exist", mount.Source)
					if err = os.MkdirAll(src, 0755); err != nil {
						err = errors.New(err.Error())
						break
					}
				} else if err != nil {
					break
				}
			}
		}
	}
	err = errors.Wrap(err, "volume directories")
	return
}*/

// Reads image ID from cached spec
func (b *LockedBundle) Image() *digest.Digest {
	if b.image == nil {
		b.image = b.bundle.Image()
	}
	return b.image
}

func (b *LockedBundle) SetParentImageId(imageID *digest.Digest) (err error) {
	if imageID == nil {
		if e := os.Remove(b.bundle.imageFile()); e != nil && !os.IsNotExist(e) {
			err = errors.New(e.Error())
		}
	} else {
		_, err = atomic.WriteFile(b.bundle.imageFile(), bytes.NewBufferString((*imageID).String()))
	}
	if err == nil {
		b.image = imageID
	} else {
		err = errors.Wrap(err, "set bundle's parent image id")
	}
	return
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
	if l, err = lock.LockFile(filepath.Clean(bundle.dir) + ".lock"); err == nil {
		err = l.TryLock()
	}
	return l, errors.Wrap(err, "lock bundle")
}

func deleteBundle(dir string) (err error) {
	if err = os.RemoveAll(dir); err != nil {
		err = errors.New("delete bundle: " + err.Error())
	}
	return
}
