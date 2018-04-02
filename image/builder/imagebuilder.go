package builder

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/mgoltzsche/cntnr/bundle"
	"github.com/mgoltzsche/cntnr/image"
	exterrors "github.com/mgoltzsche/cntnr/pkg/errors"
	"github.com/mgoltzsche/cntnr/pkg/files"
	"github.com/mgoltzsche/cntnr/pkg/idutils"
	"github.com/mgoltzsche/cntnr/pkg/log"
	"github.com/mgoltzsche/cntnr/run"
	"github.com/mgoltzsche/cntnr/run/factory"
	"github.com/opencontainers/go-digest"
	ispecs "github.com/opencontainers/image-spec/specs-go/v1"
	rspecs "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/pkg/errors"
)

type ImageBuildConfig struct {
	Images   image.ImageStoreRW
	Bundles  bundle.BundleStore
	Cache    ImageBuildCache
	Tempfs   string
	RunRoot  string
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

func (b *ImageBuilder) SetUser(user string) {
	b.addBuildStep(func(builder *BuildState) error {
		return builder.SetUser(user)
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
		err = exterrors.Append(err, state.Close())
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
	images    image.ImageStoreRW
	bundles   bundle.BundleStore
	config    ispecs.Image
	image     *image.Image
	cache     ImageBuildCache
	bundle    *bundle.LockedBundle
	container run.Container
	stdio     run.ContainerIO
	tempDir   string
	runRoot   string
	rootless  bool
	proot     string
	loggers   log.Loggers
}

func NewBuildState(cfg ImageBuildConfig) (r BuildState) {
	if cfg.Tempfs == "" {
		r.tempDir = os.TempDir()
	} else {
		r.tempDir = cfg.Tempfs
	}
	if cfg.RunRoot == "" {
		r.runRoot = "/tmp/cntnr"
	} else {
		r.runRoot = cfg.RunRoot
	}
	r.images = cfg.Images
	r.bundles = cfg.Bundles
	r.cache = cfg.Cache
	r.runRoot = cfg.RunRoot
	r.rootless = cfg.Rootless
	r.proot = cfg.PRoot
	r.loggers = cfg.Loggers
	return
}

func (b *BuildState) closeContainer() {
	if err := b.Close(); err != nil {
		b.loggers.Warn.Println(err)
	}
}

func (b *BuildState) initContainer() (err error) {
	if b.bundle != nil {
		return
	}

	// Derive bundle from image
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
	bundle, err := b.bundles.CreateBundle(bb, false)
	if err != nil {
		return errors.Wrap(err, "image builder")
	}
	b.bundle = bundle
	defer func() {
		if err != nil {
			b.bundle = nil
			bundle.Delete()
		}
	}()

	// Create container from bundle
	manager, err := factory.NewContainerManager(b.runRoot, b.rootless, b.loggers)
	if err != nil {
		return
	}
	// TODO: move container creation into bundle init method and update the process here only
	b.stdio = run.NewStdContainerIO()
	container, err := manager.NewContainer(&run.ContainerConfig{
		Id:             bundle.ID(),
		Bundle:         bundle,
		Io:             b.stdio,
		DestroyOnClose: true,
	})
	if err == nil {
		b.container = container
	}
	return
}

func (b *BuildState) SetAuthor(author string) error {
	b.config.Author = author
	return b.cached("AUTHOR "+author, b.commitConfig)
}

func (b *BuildState) SetUser(user string) (err error) {
	b.config.Config.User = user
	return b.cached("USER "+user, func(createdBy string) error {
		// Validate user
		if err := b.initContainer(); err != nil {
			return err
		}
		return b.commitConfig(createdBy)
	})
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

	createdBy := fmt.Sprintf("RUN /bin/sh -c %q", cmd)
	return b.cached(createdBy, func(createdBy string) (err error) {
		if err = b.initContainer(); err != nil {
			return
		}

		// Run bundle and create new image layer from the result
		spec, err := b.bundle.Spec()
		if err != nil {
			return
		}
		proc, err := b.newProcess(cmd, spec)
		if err != nil {
			return
		}

		if err = b.container.Exec(proc, b.stdio); err != nil {
			return
		}
		rootfs := filepath.Join(b.bundle.Dir(), spec.Root.Path)
		src, err := b.images.NewLayerSource(rootfs, nil)
		if err != nil {
			return
		}
		return b.commitLayer(src, createdBy)
	})
}

func (b *BuildState) newProcess(cmd string, spec *rspecs.Spec) (pr *rspecs.Process, err error) {
	u := idutils.ParseUser(b.config.Config.User)
	rootfs := filepath.Join(b.bundle.Dir(), spec.Root.Path)
	usr, err := u.Resolve(rootfs)
	if err != nil {
		return
	}
	p := *spec.Process
	p.User = rspecs.User{
		UID: uint32(usr.Uid),
		GID: uint32(usr.Gid),
		// TODO: resolve additional group ids
	}
	p.Args = []string{"/bin/sh", "-c", cmd}
	p.Env = b.config.Config.Env
	p.Cwd = b.config.Config.WorkingDir
	return &p, nil
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
	defer exterrors.Wrapd(&err, "copy into image")

	if len(srcPattern) == 0 {
		return
	}
	var rootfs string
	if b.bundle == nil {
		if err = os.MkdirAll(b.tempDir, 0750); err != nil {
			return errors.New(err.Error())
		}
		if rootfs, err = ioutil.TempDir(b.tempDir, ".img-build-"); err != nil {
			return errors.New(err.Error())
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
		b.closeContainer()
		return
	}

	commitSrc, err := b.images.NewLayerSource(rootfs, destFiles)
	if err != nil {
		b.closeContainer()
		return
	}
	createdBy := "COPY " + commitSrc.DiffHash().String()
	return b.cached(createdBy, func(createdBy string) (err error) {
		return b.commitLayer(commitSrc, createdBy)
	})
}

func (b *BuildState) commitLayer(src image.LayerSource, createdBy string) (err error) {
	b.loggers.Debug.Println("Committing layer ...")
	var parentImageId *digest.Digest
	if b.image != nil {
		pImgId := b.image.ID()
		parentImageId = &pImgId
	}
	img, err := b.images.AddImageLayer(src, parentImageId, b.config.Author, createdBy)
	if err == nil {
		if err = b.setImage(&img); err == nil {
			newImageId := img.ID()
			if b.bundle != nil {
				err = b.bundle.SetParentImageId(&newImageId)
			}
		}
	}
	return errors.Wrap(err, "commit layer")
}

func (b *BuildState) commitConfig(createdBy string) (err error) {
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
	if err == nil {
		err = b.setImage(&img)
	}
	return errors.Wrap(err, "commit config")
}

func (b *BuildState) AddTag(name string) (err error) {
	img, err := b.images.TagImage(b.image.ID(), name)
	if err == nil {
		b.image = &img
	}
	return
}

func (b *BuildState) cached(uniqCreatedBy string, call func(createdBy string) error) (err error) {
	defer func() {
		if err != nil {
			// Release bundle when operation failed
			b.closeContainer()
		}
	}()
	b.loggers.Info.Println(uniqCreatedBy)
	var parentImgId *digest.Digest
	if b.image != nil {
		pImgId := b.image.ID()
		parentImgId = &pImgId
	}
	var cachedImgId digest.Digest
	cachedImgId, err = b.cache.Get(parentImgId, uniqCreatedBy)
	if err == nil {
		if cachedImg, e := b.images.Image(cachedImgId); e == nil {
			// TODO: distinguish between image not found and serious error
			b.loggers.Info.WithField("img", (*b.image).ID()).Printf("Using cached image")
			err = b.setImage(&cachedImg)
			err = errors.Wrap(err, "cached image")
			return
		}
	} else if e, ok := err.(CacheError); !ok || !e.Temporary() {
		// if no "entry not found" error
		return err
	} else {
		err = nil
	}

	if err = call(uniqCreatedBy); err != nil {
		return
	}

	err = b.cache.Put(parentImgId, uniqCreatedBy, (*b.image).ID())

	b.loggers.Info.WithField("img", (*b.image).ID()).Printf("Built new image")

	return
}

func (b *BuildState) Close() (err error) {
	if b.container != nil {
		err = b.container.Close()
		errors.Wrap(err, "close image builder")
		b.container = nil
		b.bundle = nil
	}
	return
}
