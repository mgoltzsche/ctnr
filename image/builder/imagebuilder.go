package builder

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/hashicorp/go-multierror"
	"github.com/mgoltzsche/cntnr/bundle"
	"github.com/mgoltzsche/cntnr/image"
	"github.com/mgoltzsche/cntnr/pkg/files"
	"github.com/mgoltzsche/cntnr/pkg/log"
	"github.com/mgoltzsche/cntnr/run"
	"github.com/mgoltzsche/cntnr/run/factory"
	"github.com/opencontainers/go-digest"
	ispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/opencontainers/runtime-tools/generate"
	"github.com/pkg/errors"
)

type ImageBuildConfig struct {
	Images   image.ImageStoreRW
	Bundles  bundle.BundleStore
	Cache    ImageBuildCache
	Tempfs   string
	Rootless bool
	PRoot    string
	Loggers  log.Loggers
}

type ImageBuilder struct {
	steps []func(*BuildState) error
	err   error
}

func NewImageBuilder() *ImageBuilder {
	return &ImageBuilder{}
}

func (b *ImageBuilder) FromImage(image string) {
	b.addBuildStep(func(builder *BuildState) error {
		return builder.FromImage(image)
	})
}

func (b *ImageBuilder) SetAuthor(image string) {
	b.addBuildStep(func(builder *BuildState) error {
		builder.SetAuthor(image)
		return nil
	})
}

func (b *ImageBuilder) SetWorkingDir(dir string) {
	b.addBuildStep(func(builder *BuildState) error {
		return builder.SetWorkingDir(dir)
	})
}

func (b *ImageBuilder) SetEntrypoint(entrypoint []string) {
	b.addBuildStep(func(builder *BuildState) error {
		return builder.SetEntrypoint(entrypoint)
	})
}

func (b *ImageBuilder) SetCmd(cmd []string) {
	b.addBuildStep(func(builder *BuildState) error {
		return builder.SetCmd(cmd)
	})
}

func (b *ImageBuilder) Run(cmd string) {
	b.addBuildStep(func(builder *BuildState) error {
		return builder.Run(cmd)
	})
}

func (b *ImageBuilder) Copy(ctxDir string, srcPattern []string, dest string) (err error) {
	if err = files.ValidateGlob(srcPattern); err != nil {
		return
	}
	b.addBuildStep(func(builder *BuildState) error {
		return builder.CopyFile(ctxDir, srcPattern, dest)
	})
	return
}

func (b *ImageBuilder) Tag(tag string) {
	b.addBuildStep(func(builder *BuildState) error {
		return builder.Tag(tag)
	})
}

func (b *ImageBuilder) addBuildStep(step func(*BuildState) error) {
	b.steps = append(b.steps, step)
}

func (b *ImageBuilder) Build(cfg ImageBuildConfig) (img image.Image, err error) {
	defer func() {
		if err != nil {
			err = errors.Wrap(err, "build image")
		}
	}()

	if b.err != nil {
		return img, b.err
	}

	state := NewBuildState(cfg)
	defer func() {
		if e := state.Close(); e != nil {
			err = multierror.Append(err, e)
		}
	}()

	now := time.Now()
	state.config.Created = &now
	state.config.Architecture = runtime.GOARCH
	state.config.OS = runtime.GOOS

	if len(b.steps) == 0 {
		return img, errors.New("no build steps defined")
	}

	for _, step := range b.steps {
		if err = step(&state); err != nil {
			return
		}
	}

	return *state.image, nil
}

type BuildState struct {
	images   image.ImageStoreRW
	bundles  bundle.BundleStore
	config   ispecs.Image
	image    *image.Image
	cache    ImageBuildCache
	bundle   *bundle.LockedBundle
	tempdir  string
	rootless bool
	proot    string
	loggers  log.Loggers
}

func NewBuildState(cfg ImageBuildConfig) (r BuildState) {
	if cfg.Tempfs == "" {
		r.tempdir = os.TempDir()
	} else {
		r.tempdir = cfg.Tempfs
	}
	r.images = cfg.Images
	r.bundles = cfg.Bundles
	r.cache = cfg.Cache
	r.rootless = cfg.Rootless
	r.proot = cfg.PRoot
	r.loggers = cfg.Loggers
	return
}

func (b *BuildState) initBundle(cmd string) (err error) {
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
		if b.proot != "" {
			bb.SetPRootPath(b.proot)
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

func (b *BuildState) SetAuthor(author string) error {
	b.config.Author = author
	return b.cached("AUTHOR "+author, b.commitConfig)
}

func (b *BuildState) SetWorkingDir(dir string) error {
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(b.config.Config.WorkingDir, dir)
	}
	b.config.Config.WorkingDir = dir
	return b.cached("WORKDIR "+dir, b.commitConfig)
}

func (b *BuildState) SetEntrypoint(entrypoint []string) (err error) {
	entrypointJson, err := json.Marshal(entrypoint)
	if err != nil {
		return
	}
	b.config.Config.Entrypoint = entrypoint
	return b.cached("ENTRYPOINT "+string(entrypointJson), b.commitConfig)
}

func (b *BuildState) SetCmd(cmd []string) (err error) {
	cmdJson, err := json.Marshal(cmd)
	if err != nil {
		return
	}
	b.config.Config.Cmd = cmd
	return b.cached("CMD "+string(cmdJson), b.commitConfig)
}

func (b *BuildState) FromImage(image string) (err error) {
	b.loggers.Info.Println("FROM", image)
	if b.image != nil {
		return errors.New("base image must be defined as first build step")
	}
	img, e := b.images.ImageByName(image)
	// TODO: distinguish between 'image not found' and serious error
	if e != nil {
		if img, err = b.images.ImportImage(image); err != nil {
			return
		}
	}

	return b.setImage(&img)
}

func (b *BuildState) setImage(img *image.Image) (err error) {
	b.image = img
	b.config, err = img.Config()
	return
}

func (b *BuildState) Run(cmd string) (err error) {
	if b.image == nil {
		err = errors.New("cannot run a command in an empty image")
		return
	}

	comment := fmt.Sprintf("RUN /bin/sh -c %q", cmd)
	return b.cached(comment, func(comment string) (err error) {
		if err = b.initBundle(cmd); err != nil {
			return
		}

		// Run bundle and create new image layer from the result
		spec, err := b.bundle.Spec()
		if err != nil {
			return
		}
		rootfs := filepath.Join(b.bundle.Dir(), spec.Root.Path)
		manager, err := factory.NewContainerManager(rootfs, b.rootless, b.loggers)
		if err != nil {
			return
		}
		// TODO: move container creation into bundle init method and update the process here only
		container, err := manager.NewContainer(&run.ContainerConfig{
			Id:             b.bundle.ID(),
			Bundle:         b.bundle,
			Io:             run.NewStdContainerIO(),
			DestroyOnClose: true,
		})
		if err != nil {
			return
		}
		defer func() {
			if e := container.Close(); e != nil {
				err = multierror.Append(err, e)
			}
		}()

		if err = container.Start(); err != nil {
			return
		}
		if err = container.Wait(); err != nil {
			return
		}
		src, err := b.images.NewLayerSource(rootfs, nil)
		if err != nil {
			return
		}
		return b.commitLayer(src, comment)
	})
}

func (b *BuildState) Tag(tag string) (err error) {
	if b.image == nil {
		return errors.New("no image to tag provided")
	}
	img, err := b.images.TagImage(b.image.ID(), tag)
	if err == nil {
		b.image = &img
	}
	return
}

type FileEntry struct {
	Source      string
	Destination string
	// TODO: add mode
}

func (b *BuildState) CopyFile(contextDir string, srcPattern []string, dest string) (err error) {
	// TODO: build mtree diffs, merge them and let BlobStoreExt.diff create the layer without touching the bundle
	// => not possible with umoci's GenerateLayer/tarGenerator.AddFile methods
	defer func() {
		if err != nil {
			// Release bundle when operation failed
			if b.bundle != nil {
				err = multierror.Append(err, b.bundle.Close())
				b.bundle = nil
			}
			err = errors.Wrap(err, "copy file into image")
		}
	}()

	if len(srcPattern) == 0 {
		return
	}
	var rootfs string
	if b.bundle == nil {
		if err = os.MkdirAll(b.tempdir, 0750); err != nil {
			return
		}
		if rootfs, err = ioutil.TempDir(b.tempdir, ".img-build-"); err != nil {
			return
		}
		defer func() {
			if e := os.RemoveAll(rootfs); e != nil {
				b.loggers.Error.Println(e)
			}
		}()
	} else {
		s, _ := b.bundle.Spec()
		rootfs = filepath.Join(b.bundle.Dir(), s.Root.Path)
	}
	srcFiles, err := files.Glob(contextDir, srcPattern)
	if err != nil {
		return
	}

	fs := files.NewFileSystemBuilder(rootfs, b.rootless, b.loggers.Debug)
	workingDir := "/"
	if b.image != nil {
		cfg, e := b.image.Config()
		if e != nil {
			return e
		}
		workingDir = cfg.Config.WorkingDir
	}
	if !filepath.IsAbs(dest) {
		dest = filepath.Join(workingDir, dest)
	}
	destFiles, err := fs.Add(srcFiles, dest)
	if err != nil {
		return
	}

	commitSrc, err := b.images.NewLayerSource(rootfs, destFiles)
	if err != nil {
		return
	}
	comment := "COPY " + commitSrc.DiffHash().String()
	return b.cached(comment, func(comment string) (err error) {
		return b.commitLayer(commitSrc, comment)
	})
}

func (b *BuildState) commitLayer(src image.LayerSource, createdBy string) (err error) {
	defer func() {
		if err != nil {
			err = errors.Wrap(err, "commit layer")
		}
	}()

	b.loggers.Debug.Println("Committing layer ...")

	var parentImageId *digest.Digest
	if b.image != nil {
		pImgId := b.image.ID()
		parentImageId = &pImgId
	}
	img, err := b.images.AddImageLayer(src, parentImageId, b.config.Author, createdBy)
	if err != nil {
		return
	}
	if err = b.setImage(&img); err != nil {
		return
	}
	newImageId := img.ID()
	if b.bundle != nil {
		return b.bundle.SetParentImageId(&newImageId)
	}
	return
}

func (b *BuildState) commitConfig(createdBy string) (err error) {
	defer func() {
		if err != nil {
			err = errors.Wrap(err, "commit config")
		}
	}()

	b.config.History = append(b.config.History, ispecs.History{
		Author:     b.config.Author,
		CreatedBy:  createdBy,
		EmptyLayer: true,
	})
	var parentImgId *digest.Digest
	if b.image != nil {
		imgId := b.image.ID()
		parentImgId = &imgId
	}
	img, err := b.images.AddImageConfig(b.config, parentImgId)
	if err != nil {
		return
	}
	return b.setImage(&img)
}

func (b *BuildState) AddTag(name string) (err error) {
	img, err := b.images.TagImage(b.image.ID(), name)
	if err == nil {
		b.image = &img
	}
	return
}

func (b *BuildState) cached(uniqComment string, call func(comment string) error) (err error) {
	b.loggers.Info.Println(uniqComment)
	var parentImgId *digest.Digest
	if b.image != nil {
		pImgId := b.image.ID()
		parentImgId = &pImgId
	}
	var cachedImgId digest.Digest
	cachedImgId, err = b.cache.Get(parentImgId, uniqComment)
	if err == nil {
		var cachedImg image.Image
		if cachedImg, err = b.images.Image(cachedImgId); err == nil {
			// TODO: distinguish between image not found and serious error
			if err = b.setImage(&cachedImg); err != nil {
				return errors.Wrap(err, "cached image")
			}
			b.loggers.Info.WithField("img", (*b.image).ID()).Printf("Using cached image")
			return
		}
	} else if e, ok := err.(CacheError); !ok || !e.Temporary() {
		// if no "entry not found" error
		return err
	} else {
		err = nil
	}

	defer func() {
		if err != nil {
			// Release bundle when operation failed
			if b.bundle != nil {
				err = multierror.Append(err, b.bundle.Close())
				b.bundle = nil
			}
		}
	}()

	if err = call(uniqComment); err != nil {
		return
	}

	err = b.cache.Put(parentImgId, uniqComment, (*b.image).ID())

	b.loggers.Info.WithField("img", (*b.image).ID()).Printf("Built new image")

	return
}

func (b *BuildState) Close() (err error) {
	if b.bundle != nil {
		if e := b.bundle.Close(); e != nil {
			err = multierror.Append(err, e)
		}
	}
	return
}
