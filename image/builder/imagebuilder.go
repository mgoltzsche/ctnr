package builder

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"sort"
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

func NewImageBuilder(cfg ImageBuildConfig) (r *ImageBuilder) {
	r = &ImageBuilder{}
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
	now := time.Now()
	r.config.Created = &now
	r.config.Architecture = runtime.GOARCH
	r.config.OS = runtime.GOOS
	return
}

func (b *ImageBuilder) Image() *digest.Digest {
	if b.image == nil {
		return nil
	} else {
		id := b.image.ID()
		return &id
	}
}

func (b *ImageBuilder) closeContainer() (err error) {
	if b.container != nil {
		err = b.container.Close()
		b.container = nil
	} else if b.bundle != nil {
		err = b.bundle.Close()
	}
	b.bundle = nil
	return
}

func (b *ImageBuilder) initBundle() (err error) {
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
	if err == nil {
		b.bundle = bundle
	}
	return
}

func (b *ImageBuilder) initContainer() (err error) {
	if b.container != nil {
		return
	}

	if err = b.initBundle(); err != nil {
		return
	}

	// Create container from bundle
	manager, err := factory.NewContainerManager(b.runRoot, b.rootless, b.loggers)
	if err != nil {
		return
	}
	b.stdio = run.NewStdContainerIO()
	container, err := manager.NewContainer(&run.ContainerConfig{
		Id:             b.bundle.ID(),
		Bundle:         b.bundle,
		Io:             b.stdio,
		DestroyOnClose: true,
	})
	if err == nil {
		b.container = container
	}
	return
}

func (b *ImageBuilder) SetAuthor(author string) error {
	b.config.Author = author
	return b.cached("AUTHOR "+author, b.commitConfig)
}

func (b *ImageBuilder) SetUser(user string) (err error) {
	user = idutils.ParseUser(user).String()
	b.config.Config.User = user
	return b.cached("USER "+user, func(createdBy string) (err error) {
		if _, err = b.resolveUser(nil); err == nil {
			err = b.commitConfig(createdBy)
		}
		return
	})
}

func (b *ImageBuilder) AddEnv(env map[string]string) error {
	// TODO: resolve env (and arg) expressions in all config change operations (see https://docs.docker.com/engine/reference/builder/#environment-replacement)
	//       => do that in a separate Dockerfile processor
	if len(env) == 0 {
		return errors.New("no env vars provided")
	}
	l := make([]string, 0, len(env))
	for k, v := range env {
		l = append(l, k+"="+v)
	}
	sort.Strings(l)
	createdBy := "ENV"
	for _, e := range l {
		createdBy += fmt.Sprintf(" %q", e)
	}
	b.config.Config.Env = append(b.config.Config.Env, l...)
	return b.cached(createdBy, b.commitConfig)
}

func (b *ImageBuilder) SetWorkingDir(dir string) error {
	dir = filepath.Clean(dir)
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(b.config.Config.WorkingDir, dir)
	}
	b.config.Config.WorkingDir = dir
	return b.cached("WORKDIR "+dir, b.commitConfig)
}

func (b *ImageBuilder) SetEntrypoint(entrypoint []string) (err error) {
	entrypointJson, err := json.Marshal(entrypoint)
	if err != nil {
		return
	}
	b.config.Config.Entrypoint = entrypoint
	return b.cached("ENTRYPOINT "+string(entrypointJson), b.commitConfig)
}

func (b *ImageBuilder) SetCmd(cmd []string) (err error) {
	cmdJson, err := json.Marshal(cmd)
	if err != nil {
		return
	}
	b.config.Config.Cmd = cmd
	return b.cached("CMD "+string(cmdJson), b.commitConfig)
}

func (b *ImageBuilder) FromImage(imageName string) (err error) {
	b.loggers.Info.Println("FROM", imageName)
	if b.image != nil {
		return errors.New("base image must be defined as first build step")
	}
	img, err := image.GetImage(b.images, imageName)
	if err == nil {
		err = b.setImage(&img)
	}
	return
}

func (b *ImageBuilder) setImage(img *image.Image) (err error) {
	b.image = img
	b.config, err = img.Config()
	return
}

func (b *ImageBuilder) Run(cmd string) (err error) {
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
		src, err := b.images.NewLayerSource(rootfs, false)
		if err != nil {
			return
		}
		return b.commitLayer(src, createdBy)
	})
}

func (b *ImageBuilder) newProcess(cmd string, spec *rspecs.Spec) (pr *rspecs.Process, err error) {
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

func (b *ImageBuilder) Tag(tag string) (err error) {
	if b.image == nil {
		return errors.New("no image to tag provided")
	}
	img, err := b.images.TagImage(b.image.ID(), tag)
	if err == nil {
		b.image = &img
		b.loggers.Info.WithField("img", b.image.ID()).WithField("tag", tag).Println("Tagged image")
	}
	return
}

func (b *ImageBuilder) resolveUser(u *idutils.User) (usrp *idutils.UserIds, err error) {
	user := idutils.ParseUser(b.config.Config.User)
	if u != nil {
		user = *u
	}
	if user.String() == "" {
		return nil, nil
	}
	usr, err := user.ToIds()
	if err != nil {
		if err = b.initBundle(); err == nil {
			s, _ := b.bundle.Spec()
			rootfs := filepath.Join(b.bundle.Dir(), s.Root.Path)
			usr, err = user.Resolve(rootfs)
		}
	}
	usrp = &usr
	return
}

func (b *ImageBuilder) CopyFile(contextDir string, srcPattern []string, dest string, user *idutils.User) (err error) {
	defer exterrors.Wrapd(&err, "copy into image")

	if len(srcPattern) == 0 {
		return
	}

	usr, err := b.resolveUser(user)
	if err != nil {
		return
	}

	// TODO: Map user ID from container to host namespace

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
	cpPairs := files.Map(srcFiles, dest)
	fs := files.NewFileSystemBuilder(rootfs, b.rootless, b.loggers.Debug)
	for _, p := range cpPairs {
		if err = fs.Add(p.Source, p.Dest, usr); err != nil {
			err = exterrors.Append(err, b.closeContainer())
			return
		}
	}

	commitSrc, err := b.images.NewLayerSource(rootfs, b.bundle == nil)
	if err != nil {
		err = exterrors.Append(err, b.closeContainer())
		return
	}
	hash, err := commitSrc.DiffHash(fs.Files())
	if err != nil {
		return
	}
	createdBy := "COPY " + hash.String()
	return b.cached(createdBy, func(createdBy string) (err error) {
		return b.commitLayer(commitSrc, createdBy)
	})
}

func (b *ImageBuilder) commitLayer(src image.LayerSource, createdBy string) (err error) {
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

func (b *ImageBuilder) commitConfig(createdBy string) (err error) {
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
		if err = b.setImage(&img); err == nil {
			newImageId := img.ID()
			if b.bundle != nil {
				err = b.bundle.SetParentImageId(&newImageId)
			}
		}
	}
	return errors.Wrap(err, "commit config")
}

func (b *ImageBuilder) cached(uniqCreatedBy string, call func(createdBy string) error) (err error) {
	b.loggers.Info.Println(uniqCreatedBy)

	defer func() {
		if err != nil {
			// Release bundle when operation failed
			err = exterrors.Append(err, b.closeContainer())
		}
	}()

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
			b.loggers.Info.WithField("img", cachedImg.ID()).Printf("Using cached image")
			err = b.setImage(&cachedImg)
			err = errors.Wrap(err, "cached image")
			return
		}
	} else if !IsCacheKeyNotExist(err) {
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

func (b *ImageBuilder) Close() (err error) {
	err = b.closeContainer()
	err = errors.WithMessage(err, "close image builder")
	return
}
