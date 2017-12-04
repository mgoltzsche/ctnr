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
	bb, err := bundle.BuilderFromImage(newContainerId, &baseImage)
	if err != nil {
		return b, fmt.Errorf("image builder: %s", err)
	}
	if b.container, err = containers.CreateBundle(bb); err != nil {
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
			return b, fmt.Errorf("image builder: bundle's base image %q: %s", baseImageId, e)
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

/*type FileEntry struct {
	Source      string
	Destination string
}

func (b *ImageBuilder) AddFiles(files []FileEntry) (img image.Image, err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("add files: %s", err)
		}
	}()
}*/

func (b *ImageBuilder) CommitLayer(name string) (img image.Image, err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("commit layer: %s", err)
		}
	}()

	rootfs := filepath.Join(b.container.Dir(), "rootfs")
	parentImageId := b.container.Image()
	// TODO: add proper comment
	img, err = b.images.CommitImage(rootfs, name, parentImageId, b.author, "comment")
	if err != nil {
		return
	}
	c, err := img.Config()
	if err != nil {
		return
	}
	newParentImageId := img.ID()
	if err = b.container.SetParentImageId(&newParentImageId); err != nil {
		return
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
	if _, err = b.images.PutImageManifest(b.manifest); err != nil {
		return
	}
	// TODO: map image ID - move into image store to put config, manifest, imageid
	/*if err = s.imageIds.ImageId(s.config.Digest); err != nil {
		return
	}*/
	return b.images.TagImage(b.manifest.Config.Digest, name)
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
