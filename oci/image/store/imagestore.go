package store

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/containers/image/types"
	"github.com/mgoltzsche/cntnr/log"
	"github.com/mgoltzsche/cntnr/oci/image"
	"github.com/mgoltzsche/cntnr/pkg/lock"
	digest "github.com/opencontainers/go-digest"
)

const (
	AnnotationImported = "com.github.mgoltzsche.cntnr.image.imported"
)

var _ image.ImageStore = &ImageStore{}

type ImageStore struct {
	lock lock.ExclusiveLocker
	*ImageStoreRO
	systemContext *types.SystemContext
	warn          log.Logger
}

func NewImageStore(store *ImageStoreRO, systemContext *types.SystemContext, warn log.Logger) (*ImageStore, error) {
	lck, err := lock.NewExclusiveDirLocker(filepath.Join(os.TempDir(), "cntnr", "lock"))
	if err != nil {
		err = fmt.Errorf("NewImageStore: %s", err)
	}
	return &ImageStore{lck, store, systemContext, warn}, err
}

func (s *ImageStore) OpenLockedImageStore() (image.ImageStoreRW, error) {
	return NewImageStoreRW(s.lock.NewSharedLocker(), s.ImageStoreRO, s.systemContext, s.warn)
}

func (s *ImageStore) ImageGC() (err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("image gc: %s", err)
		}
	}()

	if err = s.lock.Lock(); err != nil {
		return
	}
	defer unlock(s.lock, &err)

	// Collect named transitive blobs to leave them untouched
	keep := map[digest.Digest]bool{}
	imgs, err := s.Images()
	if err != nil {
		return err
	}
	for _, img := range imgs {
		keep[img.Digest] = true
		keep[img.Manifest.Config.Digest] = true
		for _, l := range img.Manifest.Layers {
			keep[l.Digest] = true
		}
	}

	// Delete all but the named blobs
	return s.blobs.RetainBlobs(keep)
}
