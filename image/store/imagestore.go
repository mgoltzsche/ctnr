package store

import (
	"os"
	"path/filepath"
	"time"

	"github.com/containers/image/types"
	"github.com/mgoltzsche/cntnr/image"
	exterrors "github.com/mgoltzsche/cntnr/pkg/errors"
	"github.com/mgoltzsche/cntnr/pkg/lock"
	"github.com/mgoltzsche/cntnr/pkg/log"
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
	temp          string
	systemContext *types.SystemContext
	trustPolicy   TrustPolicyContext
	rootless      bool
	loggers       log.Loggers
}

func NewImageStore(store *ImageStoreRO, temp string, systemContext *types.SystemContext, trustPolicy TrustPolicyContext, rootless bool, loggers log.Loggers) (*ImageStore, error) {
	lck, err := lock.NewExclusiveDirLocker(filepath.Join(os.TempDir(), "cntnr", "lock"))
	if err != nil {
		err = errors.Wrap(err, "new image store")
	}
	return &ImageStore{lck, store, temp, systemContext, trustPolicy, rootless, loggers}, err
}

func (s *ImageStore) OpenLockedImageStore() (image.ImageStoreRW, error) {
	return s.openLockedImageStore(s.lock.NewSharedLocker())
}

func (s *ImageStore) openLockedImageStore(locker lock.Locker) (image.ImageStoreRW, error) {
	return NewImageStoreRW(locker, s.ImageStoreRO, s.temp, s.systemContext, s.trustPolicy, s.rootless, s.loggers)
}

func (s *ImageStore) DelImage(ids ...digest.Digest) (err error) {
	defer exterrors.Wrapd(&err, "del image")
	lockedStore, err := s.openLockedImageStore(s.lock)
	if err != nil {
		return
	}
	defer func() {
		err = exterrors.Append(err, lockedStore.Close())
	}()

	imgs, err := lockedStore.Images()
	if err != nil {
		return
	}
	for _, id := range ids {
		for _, img := range imgs {
			if id == img.ID() && img.Tag != nil {
				// TODO: single delete batch per repository
				if err = lockedStore.UntagImage(img.Tag.String()); err != nil {
					return
				}
			}
		}
		if err = s.imageIds.Delete(id); err != nil {
			return
		}
	}
	return
}

func (s *ImageStore) ImageGC(ttl time.Duration) (err error) {
	before := time.Now().Add(-ttl)
	defer exterrors.Wrapd(&err, "image gc")
	lockedStore, err := s.openLockedImageStore(s.lock)
	if err != nil {
		return
	}
	defer func() {
		err = exterrors.Append(err, lockedStore.Close())
	}()

	// Collect all image IDs and delete
	keepBlobs := map[digest.Digest]bool{}
	keepImgIds := map[digest.Digest]bool{}
	keepFsSpecs := map[digest.Digest]bool{}
	delIDs := map[digest.Digest]bool{}
	imgs, err := s.Images()
	if err != nil {
		return
	}
	imgMap := map[digest.Digest]*image.ImageInfo{}
	for _, img := range imgs {
		imgMap[img.ID()] = img
		if img.LastUsed.Before(before) {
			if img.Tag != nil {
				// TODO: don't delete tagged images at all but
				//   maybe introduce separate gc timeout for tags
				// TODO: single delete batch per repository
				if err = lockedStore.UntagImage(img.Tag.String()); err != nil {
					return
				}
			}
			delIDs[img.ID()] = true
		} else {
			// TODO: also keep parents of a used image to preserve build caches
			keepImgIds[img.ID()] = true
			keepBlobs[img.ID()] = true
			keepBlobs[img.ManifestDigest] = true
			for _, l := range img.Manifest.Layers {
				keepBlobs[l.Digest] = true
			}
			if conf, e := s.blobs.ImageConfig(img.Manifest.Config.Digest); e == nil {
				keepFsSpecs[chainID(conf.RootFS.DiffIDs)] = true
			}
		}
	}

	// Delete image IDs
	for delID, _ := range delIDs {
		err = exterrors.Append(err, s.imageIds.Delete(delID))
	}

	// Delete everything but the least recently used fsspecs, imageids, blobs
	err = exterrors.Append(err, s.blobs.fsspecs.Retain(keepFsSpecs))
	err = exterrors.Append(err, s.imageIds.Retain(keepImgIds))
	err = exterrors.Append(err, s.blobs.Retain(keepBlobs))
	return
}
