package store

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/hashicorp/go-multierror"
	"github.com/mgoltzsche/cntnr/pkg/atomic"
	lock "github.com/mgoltzsche/cntnr/pkg/lock"
	ispecs "github.com/opencontainers/image-spec/specs-go/v1"
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
	defer func() {
		if err != nil {
			err = fmt.Errorf("open image repo: %s", err)
		}
	}()

	if externalBlobDir != "" && !filepath.IsAbs(externalBlobDir) {
		return nil, fmt.Errorf("externalBlobDir is not an absolute path: %q", externalBlobDir)
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
				err = multierror.Append(err, fmt.Errorf("unlock image repo: %s", e))
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
			return nil, fmt.Errorf("open image repo: repo %s does not exist", dir)
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
			return r, fmt.Errorf("blob dir: %s", err)
		}
		if err = os.Symlink(externalBlobDir, blobDir); err != nil {
			return
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
			return nil, fmt.Errorf("read oci-layout: %s", err)
		}
		if err = json.Unmarshal(b, &layout); err != nil {
			return nil, fmt.Errorf("unmarshal oci-layout: %s", err)
		}
		if layout.Version != ispecs.ImageLayoutVersion {
			return nil, fmt.Errorf("unsupported oci-layout version %q", layout.Version)
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
			return nil, fmt.Errorf("unsupported index schema version %d in %s", r.index.SchemaVersion, r.indexFile)
		}
	}
	return
}

func (r *ImageRepo) Close() (err error) {
	// Unlock image repo dir
	defer func() {
		if e := r.lock.Unlock(); e != nil {
			e = fmt.Errorf("image repo: close: %s", e)
			if err == nil {
				err = e
			} else {
				err = multierror.Append(err, e)
			}
		}
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
