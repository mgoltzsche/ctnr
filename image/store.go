package image

import (
	"bytes"
	"time"

	exterrors "github.com/mgoltzsche/cntnr/pkg/errors"
	digest "github.com/opencontainers/go-digest"
	ispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
)

// TODO: Make sure store is closed before running any container to free shared lock to allow other container to be prepared
// TODO: base Commit method in BlobStore (so that mtree can move to blobstore), UnpackImage method in ImageStore

// Minimal Store interface.
// containers/storage interface is not used to ease the OCI store implementation
// which is required by unprivileged users (https://github.com/containers/storage/issues/96)

// TODO: no shared lock during whole store lifecycle
//   but shared lock on image usage mark (touch) on every image load call (separate method from listing)
//   and during the whole image import.
//   PROBLEM: How to make sure no blobs of images that are about to be registered are garbage collected?
//   => Check blob timestamp during GC -> Not reliable since blob hierarchy is registered after blobs are registered
//     => Move write methods into separate update(func(ImageWriter)) method which opens shared lock during call

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
	AddImageConfig(m ispecs.Image, parentImageId *digest.Digest) (Image, error)
	NewLayerSource(rootfs string, addOnly bool) (LayerSource, error)
	// Creates a new layer or returns errEmptyLayerDiff if nothing has changed
	AddImageLayer(src LayerSource, parentImageId *digest.Digest, author, comment string) (Image, error)
	TagImage(imageId digest.Digest, tag string) (Image, error)
	UntagImage(tag string) error
	Close() error
}

type LayerSource interface {
	DiffHash(filterPaths []string) (digest.Digest, error)
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
	}
	return store.ImageByName(image)
}

func GetImage(store ImageStoreRW, image string) (img Image, err error) {
	if img, err = GetLocalImage(store, image); IsImageNameNotExist(err) {
		img, err = store.ImportImage(image)
	}
	return
}
