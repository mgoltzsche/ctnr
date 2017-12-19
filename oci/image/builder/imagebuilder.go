package builder

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/hashicorp/go-multierror"
	"github.com/mgoltzsche/cntnr/log"
	"github.com/mgoltzsche/cntnr/oci/bundle"
	"github.com/mgoltzsche/cntnr/oci/image"
	"github.com/mgoltzsche/cntnr/run"
	ispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/opencontainers/runtime-tools/generate"
	"github.com/pkg/errors"
)

type ImageBuilder struct {
	steps []func(*buildState) error
}

func NewImageBuilder() *ImageBuilder {
	return &ImageBuilder{}
}

func (b *ImageBuilder) FromImage(image string) {
	b.steps = append(b.steps, func(state *buildState) error {
		return state.FromImage(image)
	})
}

func (b *ImageBuilder) SetAuthor(image string) {
	b.steps = append(b.steps, func(state *buildState) error {
		state.SetAuthor(image)
		return nil
	})
}

func (b *ImageBuilder) SetEntrypoint(entrypoint []string) {
	b.steps = append(b.steps, func(state *buildState) error {
		return state.SetEntrypoint(entrypoint)
	})
}

func (b *ImageBuilder) SetCmd(cmd []string) {
	b.steps = append(b.steps, func(state *buildState) error {
		return state.SetCmd(cmd)
	})
}

func (b *ImageBuilder) Run(cmd string) {
	b.steps = append(b.steps, func(state *buildState) error {
		return state.Run(cmd)
	})
}

func (b *ImageBuilder) Tag(tag string) {
	b.steps = append(b.steps, func(state *buildState) error {
		return state.Tag(tag)
	})
}

func (b *ImageBuilder) Build(images image.ImageStoreRW, bundles bundle.BundleStore, rootless bool) (img image.Image, err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("build image: %s", err)
		}
	}()
	state := buildState{images: images, bundles: bundles, rootless: rootless}
	defer func() {
		if e := state.Close(); e != nil {
			err = multierror.Append(err, e)
		}
	}()

	now := time.Now()
	state.config.Created = &now
	state.config.Author = state.author
	state.config.Architecture = runtime.GOARCH
	state.config.OS = runtime.GOOS

	if len(b.steps) == 0 {
		return img, fmt.Errorf("No build steps defined")
	}

	for _, step := range b.steps {
		if err = step(&state); err != nil {
			return
		}
	}

	return *state.image, nil
}

type buildState struct {
	images   image.ImageStoreRW
	bundles  bundle.BundleStore
	config   ispecs.Image
	image    *image.Image
	bundle   *bundle.LockedBundle
	author   string
	rootless bool
}

func (b *buildState) initBundle(cmd string) (err error) {
	entrypoint := []string{"/bin/sh", "-c"}
	if b.bundle == nil {
		var bb *bundle.BundleBuilder
		if b.image == nil {
			bb = bundle.Builder("")
		} else if bb, err = bundle.BuilderFromImage("", b.image); err != nil {
			return errors.Wrap(err, "image builder")
		}
		if b.rootless {
			bb.ToRootless()
		}
		bb.UseHostNetwork()
		bb.SetProcessEntrypoint(entrypoint)
		if cmd != "" {
			bb.SetProcessCmd([]string{cmd})
		}
		bundle, err := b.bundles.CreateBundle(bb, false)
		if err != nil {
			return errors.Wrap(err, "image builder")
		}
		b.bundle = bundle
	} else {
		if cmd != "" {
			spec, err := b.bundle.Spec()
			if err != nil {
				return err
			}
			specgen := generate.NewFromSpec(spec)
			specgen.SetProcessArgs(append(entrypoint, cmd))
			err = b.bundle.SetSpec(&specgen)
		}
	}
	return
}

func (b *buildState) SetAuthor(author string) {
	b.author = author
	b.config.Author = author
}

func (b *buildState) SetEntrypoint(entrypoint []string) (err error) {
	entrypointJson, err := json.Marshal(entrypoint)
	if err != nil {
		return
	}
	b.config.Config.Entrypoint = entrypoint
	return b.commitConfig("ENTRYPOINT " + string(entrypointJson))
}

func (b *buildState) SetCmd(cmd []string) (err error) {
	cmdJson, err := json.Marshal(cmd)
	if err != nil {
		return
	}
	b.config.Config.Cmd = cmd
	return b.commitConfig("CMD " + string(cmdJson))
}

func (b *buildState) FromImage(image string) (err error) {
	if b.image != nil {
		return fmt.Errorf("base image must be defined as first build step")
	}
	img, e := b.images.ImageByName(image)
	if e != nil {
		return e
	}
	b.image = &img
	b.config, err = img.Config()
	b.config.Author = b.author
	return
}

func (b *buildState) Run(cmd string) (err error) {
	if b.image == nil {
		err = fmt.Errorf("cannot run a command in an empty image")
		return
	}

	if err = b.initBundle(cmd); err != nil {
		return
	}

	defer func() {
		if err != nil {
			// Release bundle when operation failed
			if b.bundle != nil {
				err = multierror.Append(err, b.bundle.Close())
				b.bundle = nil
			}
			err = fmt.Errorf("run: %s", err)
		}
	}()

	// Run bundle and create new image layer from the result
	spec, err := b.bundle.Spec()
	if err != nil {
		return
	}
	container := run.NewRuncContainer(b.bundle.ID(), b.bundle, filepath.Join(b.bundle.Dir(), spec.Root.Path), log.NewStdLogger(os.Stderr))
	if err = container.Start(); err != nil {
		return
	}
	if err = container.Wait(); err != nil {
		return
	}
	cmdJson, err := json.Marshal(spec.Process.Args)
	if err != nil {
		return
	}
	return b.commitLayer("RUN " + string(cmdJson))
}

func (b *buildState) Tag(tag string) (err error) {
	if b.image == nil {
		return fmt.Errorf("no image to tag")
	}
	img, err := b.images.TagImage(b.image.ID(), tag)
	if err == nil {
		b.image = &img
	}
	return
}

/*type FileEntry struct {
	Source      string
	Destination string
	// TODO: add mode
}

func (b *ImageBuilder) AddFile(src, dest string) {
	// TODO: build mtree diffs, merge them and let BlobStoreExt.diff create the layer (without touching the bundle)
	if b.err != nil {
		return
	}
	var err error
	defer func() {
		if err != nil {
			// Release bundle when operation failed
			if b.bundle != nil {
				err = multierror.Append(err, b.bundle.Close())
				b.bundle = nil
			}
			err = fmt.Errorf("add file to image: %s", err)
			b.err = multierror.Append(b.err, err)
		}
	}()

	if err = b.initBundle(nil); err != nil {
		return
	}
	var srcFile, destFile *os.File
	dest = filepath.Join(b.bundle.Dir(), "rootfs", dest)
	// TODO: if dir copy directory
	if srcFile, err = os.Open(src); err != nil {
		return
	}
	defer srcFile.Close()

	if err = os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return
	}

	if destFile, err = os.Create(dest); err != nil {
		return
	}
	defer destFile.Close()

	if _, err = io.Copy(destFile, srcFile); err != nil {
		return
	}

	if err = destFile.Sync(); err != nil {
		return
	}

	// TODO: comment
	b.CommitLayer("add file to image")
}*/

func (b *buildState) commitLayer(comment string) (err error) {
	defer func() {
		if err != nil {
			err = errors.Wrap(err, "commit layer")
		}
	}()

	rootfs := filepath.Join(b.bundle.Dir(), "rootfs")
	parentImageId := b.bundle.Image()
	img, err := b.images.AddImageLayer(rootfs, parentImageId, b.author, comment)
	if err != nil {
		return
	}
	c, err := img.Config()
	if err != nil {
		return
	}
	b.image = &img
	newImageId := img.ID()
	if err = b.bundle.SetParentImageId(&newImageId); err != nil {
		return
	}
	b.config = c
	return
}

func (b *buildState) commitConfig(comment string) (err error) {
	defer func() {
		if err != nil {
			err = errors.Wrap(err, "commit config")
		}
	}()

	b.config.Author = b.author
	b.config.History = append(b.config.History, ispecs.History{
		CreatedBy:  b.author,
		Comment:    comment,
		EmptyLayer: false,
	})
	img, err := b.images.AddImageConfig(b.config, b.bundle.Image())
	if err != nil {
		return
	}
	c, err := img.Config()
	if err != nil {
		return
	}
	b.image = &img
	b.config = c
	return
}

func (b *buildState) AddTag(name string) (err error) {
	img, err := b.images.TagImage(b.image.ID(), name)
	if err == nil {
		b.image = &img
	}
	return
}

func (b *buildState) Close() (err error) {
	if b.bundle != nil {
		if e := b.bundle.Close(); e != nil {
			err = multierror.Append(err, e)
		}
	}
	return
}
