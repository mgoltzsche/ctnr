package store

import (
	"encoding/json"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/mgoltzsche/cntnr/pkg/atomic"
	exterrors "github.com/mgoltzsche/cntnr/pkg/errors"
	lock "github.com/mgoltzsche/cntnr/pkg/lock"
	"github.com/opencontainers/go-digest"
	ispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
)

// Writeable OCI image index representation
type LockedImageRepo struct {
	*ImageRepo
	dir        string
	extBlobDir string
	lock       lock.Unlocker
	err        error
}

func NewLockedImageRepo(name, dir, extBlobDir string) (r *LockedImageRepo, err error) {
	dir = filepath.Clean(dir)

	defer exterrors.Wrapd(&err, "open image repo")

	if extBlobDir != "" && !filepath.IsAbs(extBlobDir) {
		return nil, errors.Errorf("externalBlobDir %q is not an absolute path", extBlobDir)
	}

	// Lock repo directory
	locker, err := lock.LockFile(dir + ".lock")
	if err != nil {
		return
	}
	if err = locker.Lock(); err != nil {
		return
	}
	defer func() {
		if err != nil {
			e := locker.Unlock()
			err = exterrors.Append(err, errors.Wrap(e, "unlock image repo"))
		}
	}()

	// Load/init image index
	repo, e := NewImageRepo(name, dir)
	if e != nil {
		if os.IsNotExist(errors.Cause(e)) {
			// Start with new empty index
			repo = &ImageRepo{Name: name}
			repo.index.SchemaVersion = 2
		} else {
			return nil, e
		}
	}

	return openLockedImageRepo(repo, dir, extBlobDir, locker), nil
}

// Provides write methods to operate on an existing lock
func openLockedImageRepo(repo *ImageRepo, dir, extBlobDir string, unlocker lock.Unlocker) (r *LockedImageRepo) {
	return &LockedImageRepo{ImageRepo: repo, dir: dir, extBlobDir: extBlobDir, lock: unlocker}
}

// Writes the image index to disk and releases the lock
func (r *LockedImageRepo) Close() (err error) {
	defer func() {
		// Unlock image repo
		err = exterrors.Append(err, r.lock.Unlock())
		err = errors.Wrap(err, "close image repo")
	}()

	if err = r.err; err == nil {
		if len(r.index.Manifests) == 0 {
			// Delete whole image repo directory if manifest list is empty
			err = os.RemoveAll(r.dir)
		} else {
			// Flush the image index changes
			err = errors.Wrap(r.flush(), "flush")
		}
	}
	return
}

func (r *LockedImageRepo) flush() (err error) {
	// Create image directory if not exists
	if _, e := os.Stat(r.dir); e != nil {
		if os.IsNotExist(e) {
			if err = os.MkdirAll(r.dir, 0775); err != nil {
				return
			}
		} else {
			return e
		}
	}

	// Create/link blob dir if not exists
	blobDir := filepath.Join(r.dir, "blobs")
	if r.extBlobDir == "" {
		if err = os.MkdirAll(blobDir, 0775); err != nil {
			return
		}
	} else if _, e := os.Lstat(blobDir); os.IsNotExist(e) {
		if _, e = os.Stat(r.extBlobDir); e != nil {
			return errors.Wrap(err, "external blob dir")
		}
		if err = os.Symlink(r.extBlobDir, blobDir); err != nil {
			return errors.Wrap(err, "link external blob dir")
		}
	}

	// Create/check oci-layout
	layoutFile := filepath.Join(r.dir, "oci-layout")
	if _, e := os.Stat(layoutFile); os.IsNotExist(e) {
		// Create new oci-layout file
		layout := ispecs.ImageLayout{}
		layout.Version = ispecs.ImageLayoutVersion
		if _, err = atomic.WriteJson(layoutFile, &layout); err != nil {
			return
		}
	} else {
		// Check existing layout's version
		layout := ispecs.ImageLayout{}
		b, err := ioutil.ReadFile(layoutFile)
		if err != nil {
			return errors.Wrap(err, "read oci-layout")
		}
		if err = json.Unmarshal(b, &layout); err != nil {
			return errors.Wrap(err, "unmarshal oci-layout")
		}
		if layout.Version != ispecs.ImageLayoutVersion {
			return errors.Errorf("unsupported oci-layout version %q", layout.Version)
		}
	}

	// Write modified index.json
	_, err = atomic.WriteJson(filepath.Join(r.dir, "index.json"), &r.index)
	return
}

// Adds a manifest descriptor
func (r *LockedImageRepo) AddManifest(descriptor ispecs.Descriptor) {
	if r.err != nil {
		return
	}
	newRef := descriptor.Annotations[ispecs.AnnotationRefName]

	// Validate provided manifest descriptor
	if newRef == "" {
		r.err = errors.Errorf("add ref: no image ref defined in provided manifest descriptor (%s annotation)", ispecs.AnnotationRefName)
		return
	}
	if descriptor.Digest.Validate() != nil || descriptor.Size < 1 || descriptor.Platform.Architecture == "" || descriptor.Platform.OS == "" {
		str := ""
		if b, e := json.Marshal(&descriptor); e == nil {
			str = string(b)
		}
		r.err = errors.Errorf("add ref: invalid manifest descriptor %s", str)
		return
	}

	// Add manifest descriptor
	manifests := make([]ispecs.Descriptor, 1, len(r.index.Manifests)+1)
	manifests[0] = descriptor
	for _, m := range r.index.Manifests {
		ref := ""
		if m.Annotations != nil {
			ref = m.Annotations[ispecs.AnnotationRefName]
		}
		if !(ref == newRef && m.Platform.Architecture == descriptor.Platform.Architecture && m.Platform.OS == descriptor.Platform.OS) {
			manifests = append(manifests, m)
		}
	}
	r.index.Manifests = manifests
}

func (r *LockedImageRepo) DelManifest(ref string) {
	if r.err != nil {
		return
	}
	idx := &r.index
	if ref == "" {
		idx.Manifests = nil
	} else {
		manifests := make([]ispecs.Descriptor, 0, len(idx.Manifests))
		deleted := false
		for _, m := range idx.Manifests {
			if ref == m.Annotations[ispecs.AnnotationRefName] {
				deleted = true
			} else {
				manifests = append(manifests, m)
			}
		}
		if !deleted {
			r.err = errors.Errorf("del ref: image index has no ref %q", ref)
			return
		}
		idx.Manifests = manifests
	}
}

func (s *LockedImageRepo) Retain(keepManifests map[digest.Digest]bool) {
	if s.err != nil {
		return
	}
	kept := make([]ispecs.Descriptor, 0, len(s.index.Manifests))
	for _, m := range s.index.Manifests {
		if keepManifests[m.Digest] {
			kept = append(kept, m)
		}
	}
	s.index.Manifests = kept
}

func (s *LockedImageRepo) Limit(maxPerRepo int) {
	if maxPerRepo > 0 && len(s.index.Manifests) > maxPerRepo {
		s.index.Manifests = s.index.Manifests[:maxPerRepo]
	}
}
