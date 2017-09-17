package store

import (
	"fmt"
	"runtime"
	"time"

	digest "github.com/opencontainers/go-digest"
	ispecs "github.com/opencontainers/image-spec/specs-go/v1"
)

// Reduced Store interface.
// containers/storage interface is not used to ease the simple store implementation
// and since containers/storage currently cannot be used by unprivileged users (https://github.com/containers/storage/issues/96)
type ImageBuilder interface {
	//Run(command string) error
	CommitLayer() error
	Build(name string)
}

type imageBuilder struct {
	store     Store
	manifest  ispecs.Manifest
	config    ispecs.Image
	container Container
	author    string
}

func NewImageBuilder(store Store, baseImageId *digest.Digest, containerId, author string) (b imageBuilder, err error) {
	b = imageBuilder{store: store, author: author}
	var manifestDigest *digest.Digest

	if baseImageId == nil {
		// Create new empty image
		now := time.Now()
		b.manifest.Versioned.SchemaVersion = 2
		b.config.Created = &now
		b.config.Author = author
		b.config.Architecture = runtime.GOARCH
		b.config.OS = runtime.GOOS
	} else {
		// Load base image
		img, err := store.Image(*baseImageId)
		if err != nil {
			return b, fmt.Errorf("image builder: base image: %s", err)
		}
		manifestDigest = &img.ID
		if b.manifest, err = store.ImageManifest(img.ID); err != nil {
			return b, fmt.Errorf("image builder: base image: %s", err)
		}
		if b.config, err = store.ImageConfig(b.manifest.Config.Digest); err != nil {
			return b, fmt.Errorf("image builder: base image: %s", err)
		}
	}

	// Create container
	if containerId != "" {
		b.container, err = store.Container(containerId)
		if err != nil {
			return b, fmt.Errorf("image builder: %s", err)
		}
	} else {
		builder, err := store.CreateContainer(containerId, manifestDigest)
		if err != nil {
			return b, fmt.Errorf("image builder: %s", err)
		}
		b.container, err = builder.Build()
		if err != nil {
			return b, fmt.Errorf("image builder: %s", err)
		}
	}
	return
}

func (b *imageBuilder) CommitLayer() (err error) {
	c, err := b.store.Commit(b.container.ID, b.author, "comment") // TODO: add proper comment
	if err != nil {
		return
	}
	b.config = c.Config
	b.manifest = c.Manifest

	/*reader, err := b.store.Diff(b.container.ID())

	// Write tar layer gzip-compressed
	diffIdDigester := digest.SHA256.Digester()
	hashReader := io.TeeReader(reader, diffIdDigester.Hash())
	pipeReader, pipeWriter := io.Pipe()
	defer pipeReader.Close()

	gzw := gzip.NewWriter(pipeWriter)
	defer gzw.Close()
	go func() {
		if _, err := io.Copy(gzw, hashReader); err != nil {
			pipeWriter.CloseWithError(fmt.Errorf("compressing layer: %s", err))
			return
		}
		gzw.Close()
		pipeWriter.Close()
	}()

	layer, err := b.store.PutLayer(b.topLayerId, pipeReader)
	if err != nil {
		return
	}
	diffIdDigest := diffIdDigester.Digest()*/

	// Append layer metadata to image manifest & config
	/*now := time.Now()
	historyEntry := ispecs.History{
		Created:    &now,
		CreatedBy:  b.author,
		Comment:    "",
		EmptyLayer: false,
	}
	if b.config.RootFS.DiffIDs == nil {
		b.config.RootFS.DiffIDs = []digest.Digest{c.DiffIdDigest}
	} else {
		b.config.RootFS.DiffIDs = append(b.config.RootFS.DiffIDs, c.DiffIdDigest)
	}
	if b.config.History == nil {
		b.config.History = []ispecs.History{historyEntry}
	} else {
		b.config.History = append(b.config.History, historyEntry)
	}*/
	return
}

func (b *imageBuilder) Build(name, ref string) (img Image, err error) {
	config, err := b.store.PutImageConfig(b.config)
	if err != nil {
		return
	}
	b.manifest.Config = config
	manifest, err := b.store.PutImageManifest(b.manifest)
	if err != nil {
		return
	}
	return b.store.CreateImage(name, ref, manifest.Digest)
}
