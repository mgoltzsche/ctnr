package image

import (
	"bytes"
	"time"

	exterrors "github.com/mgoltzsche/cntnr/pkg/errors"
	"github.com/mgoltzsche/cntnr/pkg/fs"
	digest "github.com/opencontainers/go-digest"
	ispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
)

// containers/storage interface is not used to ease the OCI store implementation
// which is required by unprivileged users (https://github.com/containers/storage/issues/96)

type ImageStore interface {
	ImageStoreRO
	OpenLockedImageStore() (ImageStoreRW, error)
	ImageGC(before time.Time) error
	DelImage(id ...digest.Digest) error
}

type ImageStoreRO interface {
	Images() ([]Image, error)
	Image(id digest.Digest) (Image, error)
	ImageByName(name string) (Image, error)
}

type ImageTagStore interface {
	AddTag(name string, imageID digest.Digest) error
	DelTag(name string) error
	Tag(name string) (Tag, error)
	Tags() ([]Tag, error)
}

type ImageStoreRW interface {
	ImageStoreRO
	MarkUsedImage(imageId digest.Digest) error
	ImportImage(name string) (Image, error)
	SupportsTransport(transportName string) bool
	AddImageConfig(m ispecs.Image, parentImageId *digest.Digest) (Image, error)
	FS(imageId digest.Digest) (fs.FsNode, error)
	// Creates a new layer as diff to parent. Returns errEmptyLayerDiff if nothing has changed
	AddLayer(rootfs fs.FsNode, parentImageId *digest.Digest, author, createdByOp string) (Image, error)
	TagImage(imageId digest.Digest, tag string) (Image, error)
	UntagImage(tag string) error
	Close() error
}

type LayerSource interface {
	DiffHash() (digest.Digest, error)
	Close() error
}

const (
	errEmptyLayerDiff    = "github.com/mgoltzsche/cntnr/image/emptylayerdiff"
	errImageIdNotExist   = "github.com/mgoltzsche/cntnr/image/idnotexist"
	errImageNameNotExist = "github.com/mgoltzsche/cntnr/image/namenotexist"
)

func IsImageIdNotExist(err error) bool {
	return exterrors.HasType(err, errImageIdNotExist)
}

func ErrorImageIdNotExist(format string, o ...interface{}) error {
	return exterrors.Typedf(errImageIdNotExist, format, o...)
}

func IsImageNameNotExist(err error) bool {
	return exterrors.HasType(err, errImageNameNotExist)
}

func ErrorImageNameNotExist(format string, o ...interface{}) error {
	return exterrors.Typedf(errImageNameNotExist, format, o...)
}

func IsEmptyLayerDiff(err error) bool {
	return exterrors.HasType(err, errEmptyLayerDiff)
}

func ErrorEmptyLayerDiff(msg string) error {
	return exterrors.Typed(errEmptyLayerDiff, msg)
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
	if img, err = GetLocalImage(store, image); IsImageNameNotExist(err) {
		img, err = store.ImportImage(image)
	}
	return
}
