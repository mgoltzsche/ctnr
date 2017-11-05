package image

import (
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
	ImageGC() error
}

type ImageStoreRO interface {
	Images() ([]Image, error)
	Image(id digest.Digest) (Image, error)
	ImageByName(name string) (Image, error)
}

type ImageStoreRW interface {
	ImageStoreRO
	MarkUsedImage(id digest.Digest) error
	ImportImage(name string) (Image, error)
	PutImageManifest(m ispecs.Manifest) (ispecs.Descriptor, error)
	PutImageConfig(m ispecs.Image) (ispecs.Descriptor, error)
	CommitImage(rootfs, name string, parentManifest *digest.Digest, author, comment string) (Image, error)
	CreateImage(name string, manifestDigest digest.Digest) (Image, error)
	DeleteImage(name string) error
	Close() error
}
