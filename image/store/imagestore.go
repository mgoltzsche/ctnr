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

func (s *ImageStore) openLockedImageStore(locker lock.Locker) (*ImageStoreRW, error) {
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

func (s *ImageStore) ImageGC(ttl, refTTL time.Duration, maxPerRepo int) (err error) {
	lockedStore, err := s.openLockedImageStore(s.lock)
	if err != nil {
		return
	}
	defer func() {
		err = exterrors.Append(err, lockedStore.Close())
	}()
	return newImageGC(lockedStore, ttl, refTTL, maxPerRepo).GC()
}
