package image

import (
	"time"

	digest "github.com/opencontainers/go-digest"
	ispecs "github.com/opencontainers/image-spec/specs-go/v1"
)

type ImageReader interface {
	UnpackLayers(manifestDigest digest.Digest, rootfs string) error
	ImageConfig(d digest.Digest) (ispecs.Image, error)
}

type Image struct {
	Digest   digest.Digest
	Name     string
	Ref      string
	Manifest ispecs.Manifest
	Size     uint64
	Created  time.Time
	config   *ispecs.Image
	reader   ImageReader
}

func NewImage(id digest.Digest, name, ref string, created time.Time, manifest ispecs.Manifest, config *ispecs.Image, reader ImageReader) Image {
	var size uint64
	for _, l := range manifest.Layers {
		if l.Size > 0 {
			size += uint64(l.Size)
		}
	}
	return Image{id, name, ref, manifest, size, created, config, reader}
}

func (img *Image) ID() string {
	return img.Digest.String()
}

func (img *Image) Config() (cfg ispecs.Image, err error) {
	if img.config == nil {
		if img.reader == nil {
			panic("refused to load image config since image instance has not been loaded by locked store")
		}
		if cfg, err = img.reader.ImageConfig(img.Manifest.Config.Digest); err != nil {
			return
		}
		img.config = &cfg
	} else {
		cfg = *img.config
	}
	return
}

func (img *Image) Unpack(dest string) error {
	if img.reader == nil {
		panic("refused to unpack image since image instance has not been loaded by locked store")
	}
	return img.reader.UnpackLayers(img.Digest, dest)
}
