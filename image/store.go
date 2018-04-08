package image

import (
	"time"

	digest "github.com/opencontainers/go-digest"
	ispecs "github.com/opencontainers/image-spec/specs-go/v1"
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
	AddImageLayer(src LayerSource, parentImageId *digest.Digest, author, comment string) (Image, error)
	TagImage(imageId digest.Digest, tag string) (Image, error)
	UntagImage(tag string) error
	Close() error
}

type LayerSource interface {
	DiffHash(filterPaths []string) (digest.Digest, error)
}

func GetImage(store ImageStoreRW, image string) (img Image, err error) {
	if imgId, e := digest.Parse(image); e == nil && imgId.Validate() == nil {
		return store.Image(imgId)
	}
	img, err = store.ImageByName(image)
	// TODO: distiguish between image not found and severe error
	if err != nil {
		img, err = store.ImportImage(image)
	}
	return
}
