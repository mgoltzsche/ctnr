package store

import (
	"encoding/base32"
	"strings"
	"time"

	//"github.com/mgoltzsche/cntnr/bundle"
	digest "github.com/opencontainers/go-digest"
	ispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/satori/go.uuid"
)

// TODO: Make sure store is closed before running any container to free shared lock to allow other container to be prepared
// TODO: base Commit method in BlobStore (so that mtree can move to blobstore), UnpackImage method in ImageStore

// Minimal Store interface.
// containers/storage interface is not used to ease the OCI store implementation
// which is required by unprivileged users (https://github.com/containers/storage/issues/96)

type Store interface {
	ImageStore
	BundleStore
}

type ImageStore interface {
	ImportImage(name string) (Image, error)
	Image(id digest.Digest) (Image, error)
	ImageByName(name string) (Image, error)
	Images() ([]Image, error)
	PutImageManifest(m ispecs.Manifest) (ispecs.Descriptor, error)
	PutImageConfig(m ispecs.Image) (ispecs.Descriptor, error)
	CommitImage(rootfs, name string, parentManifest *digest.Digest, author, comment string) (Image, error)
	CreateImage(name, ref string, manifestDigest digest.Digest) (Image, error)
	DeleteImage(name, ref string) error
	ImageGC() error
	Close() error
}

type ImageReader interface {
	UnpackLayers(manifestDigest digest.Digest, rootfs string) error
	ImageConfig(d digest.Digest) (ispecs.Image, error)
}

type Image struct {
	ID       digest.Digest
	Name     string
	Ref      string
	Manifest ispecs.Manifest
	Size     uint64
	Created  time.Time
	config   *ispecs.Image
	store    ImageReader
}

func (img *Image) Config() (cfg ispecs.Image, err error) {
	if img.config == nil {
		if cfg, err = img.store.ImageConfig(img.Manifest.Config.Digest); err != nil {
			return
		}
		img.config = &cfg
	} else {
		cfg = *img.config
	}
	return
}

func (img *Image) Unpack(dest string) error {
	return img.store.UnpackLayers(img.ID, dest)
}

func NewImage(id digest.Digest, name, ref string, created time.Time, manifest ispecs.Manifest, config *ispecs.Image, store ImageReader) Image {
	var size uint64
	for _, l := range manifest.Layers {
		if l.Size > 0 {
			size += uint64(l.Size)
		}
	}
	return Image{id, name, ref, manifest, size, created, config, store}
}

type BundleStore interface {
	CreateBundle(id string, builder *BundleBuilder) (Bundle, error)
	Bundle(id string) (Bundle, error)
	Bundles() ([]Bundle, error)
	BundleGC(before time.Time) ([]Bundle, error)
}

// Generate or move into utils package since it also occurs in run
func GenerateId() string {
	return strings.TrimRight(base32.StdEncoding.EncodeToString(uuid.NewV4().Bytes()), "=")
}
