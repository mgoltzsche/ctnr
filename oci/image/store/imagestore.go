package store

import (
	"github.com/containers/image/types"
	"github.com/mgoltzsche/cntnr/oci/image"
)

const (
	AnnotationImported = "com.github.mgoltzsche.cntnr.image.imported"
)

var _ image.ImageStore = &ImageStore{}

type ImageStore struct {
	*ImageStoreRO
	systemContext *types.SystemContext
}

func NewImageStore(store *ImageStoreRO, systemContext *types.SystemContext) *ImageStore {
	return &ImageStore{store, systemContext}
}

func (s *ImageStore) OpenImageRWStore() (image.ImageStoreRW, error) {
	return NewImageStoreRW(s.ImageStoreRO, s.systemContext)
}

func (s *ImageStore) RunBatch(batch func(store image.ImageStoreRW) error) error {
	tx, err := s.OpenImageRWStore()
	if err != nil {
		return err
	}
	defer tx.Close()

	return batch(tx)
}

func (s *ImageStore) ImageGC() error {
	return s.RunBatch(func(store image.ImageStoreRW) error {
		return store.ImageGC()
	})
}
