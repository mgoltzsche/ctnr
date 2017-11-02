package builder

import (
	"fmt"
	"path/filepath"
	"runtime"
	"time"

	"github.com/mgoltzsche/cntnr/oci/bundle"
	"github.com/mgoltzsche/cntnr/oci/image"
	ispecs "github.com/opencontainers/image-spec/specs-go/v1"
)

type ImageBuilder struct {
	images    image.ImageStoreRW
	manifest  ispecs.Manifest
	config    ispecs.Image
	container *bundle.LockedBundle
	author    string
}

func NewImageBuilder(images image.ImageStoreRW, containers bundle.BundleStore, baseImage image.Image, newContainerId, author string) (b *ImageBuilder, err error) {
	b = &ImageBuilder{images: images, author: author}
	if err = b.init(&baseImage, author); err != nil {
		return
	}
	bb, err := bundle.BuilderFromImage(&baseImage)
	if err != nil {
		return b, fmt.Errorf("image builder: %s", err)
	}
	cb, err := containers.CreateBundle(newContainerId, bb)
	if err != nil {
		return b, fmt.Errorf("image builder: %s", err)
	}
	if b.container, err = cb.Lock(); err != nil {
		return b, fmt.Errorf("image builder: %s", err)
	}
	return
}

func NewImageBuilderFromBundle(images image.ImageStoreRW, container bundle.Bundle, author string) (b *ImageBuilder, err error) {
	b = &ImageBuilder{images: images, author: author}
	// Lock & load container
	if b.container, err = container.Lock(); err != nil {
		return b, fmt.Errorf("image builder: %s", err)
	}
	go func() {
		if err != nil {
			b.container.Close()
		}
	}()

	// Get base image from container
	var baseImage *image.Image
	if baseImageId := b.container.Image(); baseImageId != nil {
		img, e := images.Image(*baseImageId)
		if e != nil {
			return b, fmt.Errorf("image builder: bundle's base image %q: %s", baseImageId, err)
		}
		baseImage = &img
	}

	return b, b.init(baseImage, author)
}

func (b *ImageBuilder) init(baseImage *image.Image, author string) (err error) {
	if baseImage == nil {
		// Create new empty image
		now := time.Now()
		b.manifest.Versioned.SchemaVersion = 2
		b.config.Created = &now
		b.config.Author = author
		b.config.Architecture = runtime.GOARCH
		b.config.OS = runtime.GOOS
	} else {
		b.manifest = baseImage.Manifest
		if b.config, err = baseImage.Config(); err != nil {
			err = fmt.Errorf("image builder: init: %s", err)
		}
	}
	return
}

func (b *ImageBuilder) CommitLayer(name string) (img image.Image, err error) {
	rootfs := filepath.Join(b.container.Dir(), "rootfs")
	// TODO: add proper comment
	img, err = b.images.CommitImage(rootfs, name, b.container.Image(), b.author, "comment")
	//img, err := b.container.Commit(b.images, "", b.author, "comment")
	if err != nil {
		return img, fmt.Errorf("commit layer: %s", err)
	}
	c, err := img.Config()
	if err != nil {
		return img, fmt.Errorf("commit layer: %s", err)
	}
	if err = b.container.SetParentImageId(img.ID()); err != nil {
		return img, fmt.Errorf("commit layer: %s", err)
	}
	b.config = c
	b.manifest = img.Manifest
	return
}

func (b *ImageBuilder) Build(name string) (img image.Image, err error) {
	config, err := b.images.PutImageConfig(b.config)
	if err != nil {
		return
	}
	b.manifest.Config = config
	manifest, err := b.images.PutImageManifest(b.manifest)
	if err != nil {
		return
	}
	return b.images.CreateImage(name, manifest.Digest)
}

func (b *ImageBuilder) Close() error {
	err := b.images.Close()
	e := b.container.Close()
	if e != nil {
		if err == nil {
			err = e
		} else {
			err = fmt.Errorf("%s, %s", err, e)
		}
	}
	return err
}
