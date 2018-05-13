package builder

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mgoltzsche/cntnr/bundle"
	"github.com/mgoltzsche/cntnr/image"
	exterrors "github.com/mgoltzsche/cntnr/pkg/errors"
	"github.com/mgoltzsche/cntnr/pkg/idutils"
	"github.com/mgoltzsche/cntnr/pkg/log"
	"github.com/mgoltzsche/cntnr/run"
	"github.com/mgoltzsche/cntnr/run/factory"
	"github.com/opencontainers/go-digest"
	ispecs "github.com/opencontainers/image-spec/specs-go/v1"
	rspecs "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/pkg/errors"
)

var portsRegex = regexp.MustCompile("^(( |^)[1-9][0-9]*(/[a-z0-9]+)?)+$")

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
	images        image.ImageStoreRW
	bundles       bundle.BundleStore
	config        ispecs.Image
	image         *image.Image
	cache         ImageBuildCache
	bundle        *bundle.LockedBundle
	container     run.Container
	lockedBundles []*bundle.LockedBundle
	namedFs       map[string]string
	namedImages   map[string]image.Image
	buildNames    []string
	stdio         run.ContainerIO
	tempDir       string
	runRoot       string
	rootless      bool
	proot         string
	loggers       log.Loggers
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
	r.namedFs = map[string]string{}
	r.namedImages = map[string]image.Image{}
	return
}

func (b *ImageBuilder) Close() (err error) {
	err = b.closeContainerAndBundle()
	for _, b := range b.lockedBundles {
		err = exterrors.Append(err, b.Close())
	}
	b.lockedBundles = nil
	err = errors.WithMessage(err, "close image builder")
	return
}

func (b *ImageBuilder) closeContainerAndBundle() (err error) {
	err = b.closeContainer()
	return exterrors.Append(err, b.closeBundle())
}

func (b *ImageBuilder) closeContainer() (err error) {
	if b.container != nil {
		err = b.container.Close()
		b.container = nil
	}
	err = exterrors.Append(err, b.closeBundle())
	return
}

func (b *ImageBuilder) closeBundle() (err error) {
	if b.bundle != nil {
		err = b.bundle.Close()
		b.bundle = nil
	}
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
		b.lockedBundles = append(b.lockedBundles, bundle)
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
		DestroyOnClose: false,
	})
	if err == nil {
		b.container = container
	}
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

func (b *ImageBuilder) BuildName(name string) {
	_, fsNameExists := b.namedFs[name]
	_, imgNameExists := b.namedImages[name]
	if fsNameExists || imgNameExists {
		b.loggers.Warn.Printf("shadowing build name %q", name)
	}
	b.buildNames = append(b.buildNames, name)
}

func (b *ImageBuilder) FromImage(imageName string) (err error) {
	if err = b.closeContainer(); err != nil {
		return
	}
	if len(b.buildNames) > 0 {
		// Map name to image/bundle rootfs for efficient subsequent copy operations
		if b.bundle != nil {
			spec, err := b.bundle.Spec()
			if err != nil {
				return err
			}
			if spec.Root != nil {
				rootfs := filepath.Join(b.bundle.Dir(), spec.Root.Path)
				for _, name := range b.buildNames {
					b.namedFs[name] = rootfs
				}
				b.bundle = nil
			} else {
				b.loggers.Warn.Printf("build names %+v refer to an image without file system", b.buildNames)
			}
			b.buildNames = nil
		} else if b.image != nil {
			for _, name := range b.buildNames {
				b.namedImages[name] = *b.image
			}
			b.buildNames = nil
		}
	}
	if err = b.closeBundle(); err != nil {
		return
	}
	b.loggers.Info.Println("FROM", imageName)
	img, err := image.GetImage(b.images, imageName)
	if err == nil {
		err = b.setImage(&img)
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
	if len(env) == 0 {
		return errors.New("no env vars provided")
	}
	l := kvEntries(env)
	createdBy := "ENV"
	for _, e := range l {
		createdBy += " " + strconv.Quote(e)
	}
	b.config.Config.Env = append(b.config.Config.Env, l...)
	return b.cached(createdBy, b.commitConfig)
}

func kvEntries(m map[string]string) []string {
	l := make([]string, 0, len(m))
	for k, v := range m {
		l = append(l, k+"="+v)
	}
	sort.Strings(l)
	return l
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

func (b *ImageBuilder) SetStopSignal(signal string) (err error) {
	b.config.Config.StopSignal = signal
	return b.cached("STOPSIGNAL "+signal, b.commitConfig)
}

func (b *ImageBuilder) AddLabels(labels map[string]string) (err error) {
	if len(labels) == 0 {
		return errors.New("no labels provided")
	}
	if b.config.Config.Labels == nil {
		b.config.Config.Labels = map[string]string{}
	}
	for k, v := range labels {
		b.config.Config.Labels[k] = v
	}
	l := make([]string, 0, len(labels))
	for k, v := range labels {
		l = append(l, k+"="+v)
	}
	sort.Strings(l)
	createdBy := "LABEL"
	for _, e := range l {
		createdBy += fmt.Sprintf(" %q", e)
	}
	return b.cached(createdBy, b.commitConfig)
}

func (b *ImageBuilder) AddExposedPorts(ports []string) (err error) {
	if b.config.Config.ExposedPorts == nil {
		b.config.Config.ExposedPorts = map[string]struct{}{}
	}
	if err = ValidateExposedPorts(ports); err != nil {
		return
	}
	sort.Strings(ports)
	createdBy := "EXPOSE"
	for _, port := range ports {
		createdBy += " " + port
		b.config.Config.ExposedPorts[port] = struct{}{}
	}
	return b.cached(createdBy, b.commitConfig)
}

func (b *ImageBuilder) AddVolumes(volumes []string) (err error) {
	if b.config.Config.Volumes == nil {
		b.config.Config.Volumes = map[string]struct{}{}
	}
	sort.Strings(volumes)
	createdBy := "VOLUME"
	for _, volume := range volumes {
		createdBy += " " + volume
		b.config.Config.Volumes[volume] = struct{}{}
	}
	return b.cached(createdBy, b.commitConfig)
}

func ValidateExposedPorts(ports []string) error {
	for _, port := range ports {
		if !portsRegex.Match([]byte(port)) {
			return errors.Errorf("expecting PORT[/PROTOCOL] but was %q", port)
		}
	}
	return nil
}

func (b *ImageBuilder) setImage(img *image.Image) (err error) {
	b.image = img
	b.config, err = img.Config()
	return
}

func (b *ImageBuilder) Run(args []string, addEnv map[string]string) (err error) {
	if b.image == nil {
		err = errors.New("cannot run a command in an empty image")
		return
	}

	env := kvEntries(addEnv)
	createdBy := "RUN"
	for _, e := range env {
		createdBy += " " + strconv.Quote(e)
	}
	for _, arg := range args {
		createdBy += " " + strconv.Quote(arg)
	}
	return b.cached(createdBy, func(createdBy string) (err error) {
		if err = b.initContainer(); err != nil {
			return
		}

		// Run bundle and create new image layer from the result
		spec, err := b.bundle.Spec()
		if err != nil {
			return
		}
		proc, err := b.newProcess(args, env, spec)
		if err != nil {
			return
		}

		if err = b.container.Exec(proc, b.stdio); err != nil {
			return
		}
		rootfs := filepath.Join(b.bundle.Dir(), spec.Root.Path)
		src, err := b.images.NewLayerSource(rootfs)
		if err != nil {
			return
		}
		err = b.commitLayer(src, createdBy)
		err = exterrors.Append(err, src.Close())
		return
	})
}

func (b *ImageBuilder) newProcess(args []string, addEnv []string, spec *rspecs.Spec) (pr *rspecs.Process, err error) {
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
	p.Args = args
	p.Env = append(b.config.Config.Env, addEnv...)
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
		b.namedImages[tag] = *b.image
		b.loggers.Info.WithField("img", b.image.ID()).WithField("tag", tag).Println("Tagged image")
	}
	return
}

func (b *ImageBuilder) AddFiles(srcDir string, srcPattern []string, dest string, user *idutils.User) (err error) {
	return b.addFiles(srcPattern, dest, user, func(rootfs, dest string, usr *idutils.UserIds) (image.LayerSource, error) {
		return b.images.NewLayerSourceOverlayed(rootfs, srcDir, srcPattern, dest, usr)
	})
}

func (b *ImageBuilder) AddFilesFromImage(srcImage string, srcPattern []string, dest string, user *idutils.User) (err error) {
	if fs := b.namedFs[srcImage]; fs != "" {
		return b.AddFiles(fs, srcPattern, dest, user)
	}
	img, ok := b.namedImages[srcImage]
	if !ok {
		if img, err = image.GetImage(b.images, srcImage); err != nil {
			return
		}
	}
	return b.addFiles(srcPattern, dest, user, func(rootfs, dest string, usr *idutils.UserIds) (image.LayerSource, error) {
		return b.images.NewLayerSourceFromImage(rootfs, img, srcPattern, dest, usr)
	})
}

type layerSourceFactory func(string, string, *idutils.UserIds) (image.LayerSource, error)

func (b *ImageBuilder) addFiles(srcPattern []string, dest string, user *idutils.User, factory layerSourceFactory) (err error) {
	// TODO: also support HTTP URLs as src
	// TODO: unpack local archives when provided as source
	defer exterrors.Wrapd(&err, "add files")
	if len(srcPattern) == 0 {
		return
	}
	rootfs, dest, usr, err := b.fsState(dest, user)
	if err != nil {
		return
	}
	src, err := factory(rootfs, dest, usr)
	if err != nil {
		err = exterrors.Append(err, b.closeContainerAndBundle())
		return
	}
	defer func() {
		err = exterrors.Append(err, src.Close())
	}()
	hash, err := src.DiffHash()
	if err != nil {
		return
	}
	createdBy := "COPY " + hash.String()
	return b.cached(createdBy, func(createdBy string) (err error) {
		return b.commitLayer(src, createdBy)
	})
}

func (b *ImageBuilder) fsState(dest string, user *idutils.User) (rootfs, dir string, usr *idutils.UserIds, err error) {
	if rootfs, err = b.getRootfs(); err == nil {
		if dir, err = b.resolveDestDir(dest); err == nil {
			usr, err = b.resolveUser(user)
		}
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

func (b *ImageBuilder) getRootfs() (rootfs string, err error) {
	if b.bundle == nil {
		return
	}
	s, err := b.bundle.Spec()
	if err == nil {
		rootfs = filepath.Join(b.bundle.Dir(), s.Root.Path)
	}
	return
}

// Resolve dir against current image's working dir
func (b *ImageBuilder) resolveDestDir(dir string) (r string, err error) {
	if filepath.IsAbs(dir) {
		r = dir
	} else {
		workingDir := "/"
		if b.image != nil {
			cfg, e := b.image.Config()
			if e != nil {
				return "", e
			}
			workingDir = cfg.Config.WorkingDir
		}
		r = filepath.Join(workingDir, dir)
		if len(dir) > 0 && strings.LastIndex(dir, "/") == len(dir)-1 {
			r += "/"
		}
	}
	return
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
			err = exterrors.Append(err, b.closeContainerAndBundle())
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
		cachedImg, e := b.images.Image(cachedImgId)
		if e == nil {
			b.loggers.Info.WithField("img", cachedImg.ID()).Printf("Using cached image")
			err = b.setImage(&cachedImg)
			err = errors.Wrap(err, "cached image")
			return
		} else if !image.IsImageIdNotExist(e) {
			return errors.WithMessage(e, "load cached image")
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
