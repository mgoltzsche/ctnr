package store

import (
	"fmt"
	"runtime"
	"time"

	ispecs "github.com/opencontainers/image-spec/specs-go/v1"
)

type ImageBuilder interface {
	//Run(command string) error
	CommitLayer() error
	Build(name string)
}

type imageBuilder struct {
	images     ImageStore
	containers BundleStore
	manifest   ispecs.Manifest
	config     ispecs.Image
	container  *LockedBundle
	author     string
}

func NewImageBuilder(images ImageStore, containers BundleStore, baseImage *Image, containerId, author string) (b imageBuilder, err error) {
	b = imageBuilder{images: images, containers: containers, author: author}
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
			return b, fmt.Errorf("image builder: base image: %s", err)
		}
	}

	// Create container
	var bundle Bundle
	if containerId != "" {
		bundle, err = containers.Bundle(containerId)
		if err != nil {
			return b, fmt.Errorf("image builder: %s", err)
		}
		b.container, err = bundle.Lock()
	} else {
		var bb *BundleBuilder
		if baseImage == nil {
			bb = NewBundleBuilder()
		} else {
			bb, err = FromImage(baseImage)
			if err != nil {
				return b, fmt.Errorf("image builder: %s", err)
			}
		}
		bundle, err = containers.CreateBundle(containerId, bb)
		if err != nil {
			return b, fmt.Errorf("image builder: %s", err)
		}
	}
	b.container, err = bundle.Lock()
	if err != nil {
		return b, fmt.Errorf("image builder: %s", err)
	}
	return
}

func (b *imageBuilder) CommitLayer() (err error) {
	img, err := b.container.Commit(b.images, "", b.author, "comment") // TODO: add proper comment
	if err != nil {
		return
	}
	c, err := img.Config()
	if err != nil {
		return fmt.Errorf("commit layer: %s", err)
	}
	b.config = c
	b.manifest = img.Manifest
	return
}

func (b *imageBuilder) Build(name, ref string) (img Image, err error) {
	config, err := b.images.PutImageConfig(b.config)
	if err != nil {
		return
	}
	b.manifest.Config = config
	manifest, err := b.images.PutImageManifest(b.manifest)
	if err != nil {
		return
	}
	return b.images.CreateImage(name, ref, manifest.Digest)
}

func (b *imageBuilder) Close() error {
	return b.container.Close()
}
