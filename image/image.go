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

type ImageInfo struct {
	Tag            *TagName
	ManifestDigest digest.Digest
	Manifest       ispecs.Manifest
	Created        time.Time
	LastUsed       time.Time
}

func NewImageInfo(manifestDigest digest.Digest, manifest ispecs.Manifest, name *TagName, created, lastUsed time.Time) ImageInfo {
	return ImageInfo{name, manifestDigest, manifest, created, lastUsed}
}

func (img *ImageInfo) ID() digest.Digest {
	return img.Manifest.Config.Digest
}

func (img *ImageInfo) Size() (size uint64) {
	for _, l := range img.Manifest.Layers {
		if l.Size > 0 {
			size += uint64(l.Size)
		}
	}
	return
}

type Image struct {
	ImageInfo
	Config ispecs.Image
	reader ImageReader
}

type TagName struct {
	Repo string
	Ref  string
}

func (t *TagName) String() string {
	s := "<no tag>"
	if t != nil {
		s = t.Repo + ":" + t.Ref
	}
	return s
}

func NewImage(info ImageInfo, config ispecs.Image, reader ImageReader) Image {
	return Image{info, config, reader}
}

func (img *Image) Unpack(dest string) error {
	if img.reader == nil {
		panic("refused to unpack image since image instance has not been loaded by locked store")
	}
	return img.reader.UnpackImageLayers(img.ID(), dest)
}
