package image

import (
	"time"

	digest "github.com/opencontainers/go-digest"
	ispecs "github.com/opencontainers/image-spec/specs-go/v1"
)

type ImageReader interface {
	UnpackImageLayers(id digest.Digest, rootfs string) error
	ImageConfig(id digest.Digest) (ispecs.Image, error)
}

type Image struct {
	ManifestDigest digest.Digest
	Repo           string
	Ref            string // TODO: decide, clean up
	//Tag      TagName
	Manifest ispecs.Manifest
	Size     uint64
	Created  time.Time
	LastUsed time.Time
	config   *ispecs.Image
	reader   ImageReader
}

type TagName struct {
	Repo string
	Ref  string
}

type Tag struct {
	Name    TagName
	ImageID digest.Digest
}

func NewImage(manifestDigest digest.Digest, repo, ref string, created, lastUsed time.Time, manifest ispecs.Manifest, config *ispecs.Image, reader ImageReader) Image {
	var size uint64
	for _, l := range manifest.Layers {
		if l.Size > 0 {
			size += uint64(l.Size)
		}
	}
	return Image{manifestDigest, repo, ref, manifest, size, created, lastUsed, config, reader}
}

func (img *Image) ID() digest.Digest {
	return img.Manifest.Config.Digest
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
	return img.reader.UnpackImageLayers(img.ID(), dest)
}
