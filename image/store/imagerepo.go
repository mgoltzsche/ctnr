package store

import (
	"encoding/json"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/hashicorp/go-multierror"
	"github.com/mgoltzsche/cntnr/pkg/atomic"
	exterrors "github.com/mgoltzsche/cntnr/pkg/errors"
	lock "github.com/mgoltzsche/cntnr/pkg/lock"
	ispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
)

type ImageRepo struct {
	extBlobDir string
	indexFile  string
	index      ispecs.Index
	lock       lock.Locker
}

func OpenImageRepo(dir, externalBlobDir string, create bool) (r *ImageRepo, err error) {
	dir = filepath.Clean(dir)
	r = &ImageRepo{extBlobDir: externalBlobDir}
	defer exterrors.Wrapd(&err, "open image repo")

	if externalBlobDir != "" && !filepath.IsAbs(externalBlobDir) {
		return nil, errors.Errorf("externalBlobDir %q is not an absolute path", externalBlobDir)
	}

	// Lock repo directory
	// TODO: Use tmpfs
	r.lock, err = lock.LockFile(dir + ".lock")
	if err != nil {
		return
	}
	if err = r.lock.Lock(); err != nil {
		return
	}
	defer func() {
		if err != nil {
			if e := r.lock.Unlock(); e != nil {
				err = multierror.Append(err, errors.Wrap(e, "unlock image repo"))
			}
		}
	}()

	// Create image directory if not exists
	if _, e := os.Stat(dir); os.IsNotExist(e) {
		if create {
			if err = os.MkdirAll(dir, 0775); err != nil {
				return
			}
		} else {
			return nil, errors.Errorf("repo dir %s does not exist", dir)
		}
	}

	// Create/link blob dir if not exists
	blobDir := filepath.Join(dir, "blobs")
	if externalBlobDir == "" {
		if err = os.MkdirAll(blobDir, 0755); err != nil {
			return
		}
	} else if _, e := os.Lstat(blobDir); os.IsNotExist(e) {
		if _, e = os.Stat(externalBlobDir); os.IsNotExist(e) {
			return r, errors.Errorf("external blob dir %s does not exist", externalBlobDir)
		}
		if err = os.Symlink(externalBlobDir, blobDir); err != nil {
			return r, errors.Wrap(err, "link external blob dir")
		}
	}

	// Create/check oci-layout
	layoutFile := filepath.Join(dir, "oci-layout")
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
			return nil, errors.Wrap(err, "read oci-layout")
		}
		if err = json.Unmarshal(b, &layout); err != nil {
			return nil, errors.Wrap(err, "unmarshal oci-layout")
		}
		if layout.Version != ispecs.ImageLayoutVersion {
			return nil, errors.Errorf("unsupported oci-layout version %q", layout.Version)
		}
	}

	// Create/load index.json
	r.indexFile = filepath.Join(dir, "index.json")
	if _, e := os.Stat(r.indexFile); os.IsNotExist(e) {
		r.index.SchemaVersion = 2
	} else {
		if err = imageIndex(dir, &r.index); err != nil {
			return
		}
		if r.index.SchemaVersion != 2 {
			return nil, errors.Errorf("unsupported index schema version %d in %s", r.index.SchemaVersion, r.indexFile)
		}
	}
	return
}

func (r *ImageRepo) Close() (err error) {
	// Unlock image repo dir
	defer func() {
		if e := r.lock.Unlock(); e != nil {
			if err == nil {
				err = e
			} else {
				err = multierror.Append(err, e)
			}
		}
		err = errors.Wrap(err, "close image repo")
	}()

	// Delete whole image repo directory if manifest list is empty
	if len(r.index.Manifests) == 0 {
		return os.RemoveAll(filepath.Dir(r.indexFile))
	}

	// Write modified index.json
	_, err = atomic.WriteJson(r.indexFile, &r.index)
	return
}

func (r *ImageRepo) AddRef(descriptor ispecs.Descriptor) {
	filtered := make([]ispecs.Descriptor, 0, len(r.index.Manifests)+1)
	filtered = append(filtered, descriptor)
	newRef := descriptor.Annotations[ispecs.AnnotationRefName]
	for _, m := range r.index.Manifests {
		ref := m.Annotations[ispecs.AnnotationRefName]
		if !(ref == newRef && m.Platform.Architecture == descriptor.Platform.Architecture && m.Platform.OS == descriptor.Platform.OS) {
			filtered = append(filtered, m)
		}
	}
	r.index.Manifests = filtered
}

func (r *ImageRepo) DelRef(ref string) {
	filtered := make([]ispecs.Descriptor, 0, len(r.index.Manifests))
	for _, m := range r.index.Manifests {
		if ref != m.Annotations[ispecs.AnnotationRefName] {
			filtered = append(filtered, m)
		}
	}
	r.index.Manifests = filtered
}
