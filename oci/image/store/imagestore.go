package store

import (
	"os"
	"path/filepath"
	"time"

	"github.com/containers/image/types"
	"github.com/mgoltzsche/cntnr/log"
	"github.com/mgoltzsche/cntnr/oci/image"
	exterrors "github.com/mgoltzsche/cntnr/pkg/errors"
	"github.com/mgoltzsche/cntnr/pkg/lock"
	digest "github.com/opencontainers/go-digest"
	"github.com/pkg/errors"
)

const (
	AnnotationImported = "com.github.mgoltzsche.cntnr.image.imported"
)

var _ image.ImageStore = &ImageStore{}

type ImageStore struct {
	lock lock.ExclusiveLocker
	*ImageStoreRO
	systemContext *types.SystemContext
	trustPolicy   TrustPolicyContext
	warn          log.Logger
}

func NewImageStore(store *ImageStoreRO, systemContext *types.SystemContext, trustPolicy TrustPolicyContext, warn log.Logger) (*ImageStore, error) {
	lck, err := lock.NewExclusiveDirLocker(filepath.Join(os.TempDir(), "cntnr", "lock"))
	if err != nil {
		err = errors.Wrap(err, "new image store")
	}
	return &ImageStore{lck, store, systemContext, trustPolicy, warn}, err
}

func (s *ImageStore) OpenLockedImageStore() (image.ImageStoreRW, error) {
	return s.openLockedImageStore(s.lock.NewSharedLocker())
}

func (s *ImageStore) openLockedImageStore(locker lock.Locker) (image.ImageStoreRW, error) {
	return NewImageStoreRW(locker, s.ImageStoreRO, s.systemContext, s.trustPolicy, s.warn)
}

func (s *ImageStore) ImageGC(before time.Time) (err error) {
	defer exterrors.Wrapd(&err, "image gc")
	lockedStore, err := s.openLockedImageStore(s.lock)
	if err != nil {
		return
	}
	// TODO: close safely
	defer lockedStore.Close()

	// Collect all image IDs and delete
	keep := map[digest.Digest]bool{}
	delIDs := map[digest.Digest]bool{}
	imgs, err := s.Images()
	if err != nil {
		return
	}
	for _, img := range imgs {
		if img.LastUsed.Before(before) {
			if img.Repo != "" {
				// TODO: single delete batch per repository
				if err = lockedStore.UntagImage(img.Repo + ":" + img.Ref); err != nil {
					return
				}
			}
			delIDs[img.ID()] = true
		} else {
			keep[img.ManifestDigest] = true
			keep[img.Manifest.Config.Digest] = true
			for _, l := range img.Manifest.Layers {
				keep[l.Digest] = true
			}
		}
	}

	// Delete image IDs
	for delID, _ := range delIDs {
		if err = s.imageIds.Del(delID); err != nil {
			return
		}
	}

	// Delete all but the named blobs
	return s.blobs.RetainBlobs(keep)
}
