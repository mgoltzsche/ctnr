package bundle

import (
	"bytes"
	"encoding/json"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mgoltzsche/ctnr/pkg/atomic"
	exterrors "github.com/mgoltzsche/ctnr/pkg/errors"
	"github.com/mgoltzsche/ctnr/pkg/lock"
	"github.com/openSUSE/umoci/pkg/fseval"
	digest "github.com/opencontainers/go-digest"
	rspecs "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/pkg/errors"
)

const ANNOTATION_BUNDLE_ID = "com.github.mgoltzsche.ctnr.bundle.id"

type Bundle struct {
	id      string
	dir     string
	created time.Time
}

type SpecGenerator interface {
	Spec(rootfs string) (*rspecs.Spec, error)
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
		if err == nil && d.Validate() == nil {
			return &d
		}
	}
	return nil
}

func (b *Bundle) imageFile() string {
	return filepath.Join(b.dir, "rootfs.image")
}

func (b *Bundle) Lock() (lb *LockedBundle, err error) {
	lb, err = OpenLockedBundle(*b)
	if err == nil {
		err = b.MarkUsed()
	}
	return
}

func (b *Bundle) LastUsed() (t time.Time, err error) {
	fi, err := os.Stat(b.dir)
	if err != nil {
		return t, errors.Wrapf(err, "bundle %s last used", b.id)
	}
	return fi.ModTime(), nil
}

// Update mod time so that gc doesn't touch it for a while
func (b *Bundle) MarkUsed() (err error) {
	now := time.Now()
	if e := os.Chtimes(b.dir, now, now); e != nil && !os.IsNotExist(e) {
		err = errors.New("reset bundle expiry time: " + e.Error())
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
	return &LockedBundle{bundle, nil, nil, lck}, err
}

func CreateLockedBundle(dir string, update bool) (r *LockedBundle, err error) {
	// Create bundle
	id := filepath.Base(dir)
	bundle := Bundle{id, dir, time.Now()}

	// Lock bundle
	lck, err := lockBundle(&bundle)
	if err != nil {
		return nil, errors.Wrap(err, "create bundle")
	}
	r = &LockedBundle{bundle, nil, nil, lck}
	defer func() {
		if err != nil {
			err = exterrors.Append(err, r.Close())
		}
		err = errors.Wrap(err, "create bundle")
	}()

	// Create or update bundle
	err = os.Mkdir(dir, 0770)
	exists := err != nil && os.IsExist(err)

	if exists {
		if !update {
			return r, errors.Errorf("bundle %q already exists", id)
		}
		if err = bundle.MarkUsed(); err != nil {
			return
		}
	} else {
		if err != nil {
			return
		}
		if !update {
			defer func() {
				if err != nil {
					err = exterrors.Append(err, os.RemoveAll(dir))
				}
			}()
		}
	}

	r.spec = &rspecs.Spec{
		Root:        &rspecs.Root{Path: "rootfs"},
		Annotations: map[string]string{ANNOTATION_BUNDLE_ID: id},
	}

	return
}

func (b *LockedBundle) Close() (err error) {
	if b.lock != nil {
		err = exterrors.Append(err, errors.Wrap(b.lock.Unlock(), "unlock bundle"))
		b.lock = nil
	}
	return
}

func (b *LockedBundle) ID() string {
	return b.bundle.id
}

func (b *LockedBundle) Dir() string {
	b.checkLocked()
	return b.bundle.dir
}

func (b *LockedBundle) Spec() (*rspecs.Spec, error) {
	b.checkLocked()
	if b.spec == nil {
		spec, err := b.bundle.loadSpec()
		if err != nil {
			return nil, err
		}
		b.spec = &spec
	}
	return b.spec, nil
}

// Returns the bundle's image ID
func (b *LockedBundle) Image() *digest.Digest {
	b.checkLocked()
	if b.image == nil {
		b.image = b.bundle.Image()
	}
	return b.image
}

func (b *LockedBundle) Delete() (err error) {
	b.checkLocked()
	return exterrors.Append(DeleteDirSafely(b.Dir()), b.Close())
}

// Updates the rootfs if the image changed
func (b *LockedBundle) UpdateRootfs(image BundleImage) (err error) {
	b.checkLocked()
	var (
		rootfs    = filepath.Join(b.Dir(), "rootfs")
		imgId     *digest.Digest
		lastImgId = b.Image()
	)
	if _, e := os.Stat(rootfs); e == nil && (lastImgId == nil && image == nil || lastImgId != nil && *lastImgId == image.ID()) {
		return // don't update since the bundle is already based on the provided image
	}
	if image != nil {
		id := image.ID()
		imgId = &id
	}
	if err = DeleteDirSafely(rootfs); err != nil && !os.IsNotExist(err) {
		return
	}
	if err = image.Unpack(rootfs); err != nil {
		return
	}
	return b.SetParentImageId(imgId)
}

func (b *LockedBundle) SetParentImageId(imageID *digest.Digest) (err error) {
	b.checkLocked()
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
		err = errors.Wrapf(err, "set bundle's (%s) parent image id", b.ID())
	}
	return
}

func (b *LockedBundle) SetSpec(spec *rspecs.Spec) (err error) {
	b.checkLocked()
	if err = createVolumeDirectories(spec, b.Dir()); err != nil {
		return errors.Wrap(err, "set bundle spec")
	}
	confFile := filepath.Join(b.Dir(), "config.json")
	if _, err = atomic.WriteJson(confFile, spec); err != nil {
		return errors.Wrapf(err, "write bundle %q spec", b.ID())
	}
	b.spec = spec
	return
}

func (b *LockedBundle) checkLocked() {
	if b.lock == nil {
		panic("bundle accessed after unlocked")
	}
}

func createVolumeDirectories(spec *rspecs.Spec, dir string) (err error) {
	if spec != nil && spec.Mounts != nil {
		for _, mount := range spec.Mounts {
			if mount.Type == "bind" {
				src := mount.Source
				if !filepath.IsAbs(src) {
					src = filepath.Join(dir, src)
				}
				relsrc := filepath.Clean(mount.Source)
				if _, err = os.Stat(src); os.IsNotExist(err) {
					withinBundleDir := !filepath.IsAbs(relsrc) && strings.Index(relsrc+string(filepath.Separator), ".."+string(filepath.Separator)) != 0
					if withinBundleDir {
						if err = os.MkdirAll(src, 0755); err != nil {
							break
						}
					} else {
						err = errors.Errorf("bind mount source %q does not exist", mount.Source)
					}
				} else if err != nil {
					break
				}
			}
		}
	}
	if err != nil {
		err = errors.New("volume directories: " + err.Error())
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

func DeleteDirSafely(dir string) (err error) {
	// TODO: FsEval impl should be provided from outside
	if err = fseval.RootlessFsEval.RemoveAll(dir); err != nil {
		err = errors.New("delete dir: " + err.Error())
	}
	return
}
