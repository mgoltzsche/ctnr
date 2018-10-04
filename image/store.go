package image

import (
	"bytes"
	"time"

	"github.com/mgoltzsche/cntnr/pkg/fs"
	"github.com/opencontainers/go-digest"
	ispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
)

// containers/storage interface is not used to ease the OCI store implementation
// which is required by unprivileged users (https://github.com/containers/storage/issues/96)

type ImageStore interface {
	ImageStoreRO
	OpenLockedImageStore() (ImageStoreRW, error)
	ImageGC(ttl time.Duration) error
	DelImage(id ...digest.Digest) error
}

type ImageStoreRO interface {
	Images() ([]*ImageInfo, error)
	Image(id digest.Digest) (Image, error)
	ImageByName(name string) (Image, error)
}

type ImageStoreRW interface {
	ImageStoreRO
	ImageUnpacker
	ImportImage(name string) (Image, error)
	SupportsTransport(transportName string) bool
	AddImageConfig(m ispecs.Image, parentImageId *digest.Digest) (Image, error)
	FS(imageId digest.Digest) (fs.FsNode, error)
	// Creates a new layer as diff to parent. Returns errEmptyLayerDiff if nothing has changed
	AddLayer(rootfs fs.FsNode, parentImageId *digest.Digest, author, createdByOp string) (Image, error)
	TagImage(imageId digest.Digest, tag string) (ImageInfo, error)
	UntagImage(tag string) error
	Close() error
}

type ImageUnpacker interface {
	UnpackImageLayers(id digest.Digest, rootfs string) error
}

type LayerSource interface {
	DiffHash() (digest.Digest, error)
	Close() error
}

type ErrNotExist error

type ErrEmptyLayerDiff error

func IsNotExist(err error) bool {
	switch errors.Cause(err).(type) {
	case ErrNotExist:
		return true
	}
	return false
}

func IsEmptyLayerDiff(err error) bool {
	switch errors.Cause(err).(type) {
	case ErrEmptyLayerDiff:
		return true
	}
	return false
}

func GetLocalImage(store ImageStoreRO, image string) (img Image, err error) {
	if len(bytes.TrimSpace([]byte(image))) == 0 {
		return img, errors.New("get image: no image specified")
	}
	if imgId, e := digest.Parse(image); e == nil && imgId.Validate() == nil {
		return store.Image(imgId)
	} else {
		return store.ImageByName(image)
	}
}

func GetImage(store ImageStoreRW, image string) (img Image, err error) {
	if img, err = GetLocalImage(store, image); IsNotExist(err) {
		img, err = store.ImportImage(image)
	}
	return
}
