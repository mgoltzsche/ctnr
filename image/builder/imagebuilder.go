package builder

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mgoltzsche/ctnr/bundle"
	"github.com/mgoltzsche/ctnr/image"
	exterrors "github.com/mgoltzsche/ctnr/pkg/errors"
	"github.com/mgoltzsche/ctnr/pkg/fs"
	"github.com/mgoltzsche/ctnr/pkg/fs/tree"
	"github.com/mgoltzsche/ctnr/pkg/fs/writer"
	"github.com/mgoltzsche/ctnr/pkg/idutils"
	"github.com/mgoltzsche/ctnr/pkg/log"
	"github.com/mgoltzsche/ctnr/run"
	"github.com/mgoltzsche/ctnr/run/factory"
	"github.com/opencontainers/go-digest"
	ispecs "github.com/opencontainers/image-spec/specs-go/v1"
	rspecs "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/pkg/errors"
)

var portsRegex = regexp.MustCompile("^(( |^)[1-9][0-9]*(/[a-z0-9]+)?)+$")

type ImageBuildConfig struct {
	Images                 image.ImageStoreRW
	Bundles                bundle.BundleStore
	Cache                  ImageBuildCache
	Tempfs                 string
	RunRoot                string
	Rootless               bool
	PRoot                  string
	RemoveSucceededBundles bool
	RemoveFailedBundle     bool
	Loggers                log.Loggers
}

type ImageBuilder struct {
	images                 image.ImageStoreRW
	bundles                bundle.BundleStore
	imageResolver          ImageResolver
	config                 ispecs.Image
	image                  *image.Image
	cache                  ImageBuildCache
	bundle                 *bundle.LockedBundle
	container              run.Container
	lockedBundles          []*bundle.LockedBundle
	namedFs                map[string]*imageFs
	namedImages            map[string]*image.Image
	tmpImgFsDirs           []string
	buildNames             []string
	fsBuilder              *tree.FsBuilder
	stdio                  run.ContainerIO
	tempDir                string
	runRoot                string
	rootless               bool
	proot                  string
	removeSucceededBundles bool
	removeFailedBundle     bool
	loggers                log.Loggers
}

type imageFs struct {
	rootfs  string
	imageId digest.Digest
}

type ImageResolver func(store image.ImageStoreRW, image string) (img image.Image, err error)

func ResolveDockerImage(store image.ImageStoreRW, imageRef string) (img image.Image, err error) {
	if img, err = image.GetLocalImage(store, imageRef); image.IsNotExist(err) {
		transport := ""
		if p := strings.Index(imageRef, ":"); p > 0 {
			transport = imageRef[:p]
		}
		if !store.SupportsTransport(transport) {
			imageRef = "docker://" + imageRef
			img, err = image.GetLocalImage(store, imageRef)
		}
		if image.IsNotExist(err) {
			img, err = store.ImportImage(imageRef)
		}
	}
	return
}

func NewImageBuilder(cfg ImageBuildConfig) (r *ImageBuilder) {
	r = &ImageBuilder{}
	r.imageResolver = image.GetImage
	if cfg.Tempfs == "" {
		r.tempDir = os.TempDir()
	} else {
		r.tempDir = cfg.Tempfs
	}
	if cfg.RunRoot == "" {
		r.runRoot = "/tmp/ctnr"
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
	r.initConfig()
	r.namedFs = map[string]*imageFs{}
	r.namedImages = map[string]*image.Image{}
	r.removeSucceededBundles = cfg.RemoveSucceededBundles
	r.removeFailedBundle = cfg.RemoveFailedBundle
	return
}

func (b *ImageBuilder) initConfig() {
	now := time.Now()
	b.config.Created = &now
	b.config.Architecture = runtime.GOARCH
	b.config.OS = runtime.GOOS
	b.config.RootFS.Type = "layers"
}

func (b *ImageBuilder) SetImageResolver(r ImageResolver) {
	b.imageResolver = r
}

func (b *ImageBuilder) closeBundle(lb *bundle.LockedBundle) error { return lb.Close() }

func (b *ImageBuilder) deleteBundle(lb *bundle.LockedBundle) error { return lb.Delete() }

func (b *ImageBuilder) Close() (err error) {
	succeededBundles := b.lockedBundles
	var failedBundle *bundle.LockedBundle
	hasFailedBundle := b.bundle == nil && len(succeededBundles) > 0
	if hasFailedBundle {
		failedBundle = succeededBundles[len(succeededBundles)-1]
		succeededBundles = succeededBundles[:len(succeededBundles)-1]
	}
	err = exterrors.Append(err, b.resetBundle())
	closeBundle := b.closeBundle
	if b.removeSucceededBundles {
		closeBundle = b.deleteBundle
	}
	for _, lb := range succeededBundles {
		err = exterrors.Append(err, closeBundle(lb))
	}
	if failedBundle != nil {
		closeBundle = b.closeBundle
		if b.removeFailedBundle {
			closeBundle = b.deleteBundle
		}
		err = exterrors.Append(err, closeBundle(failedBundle))
	}
	b.lockedBundles = nil
	for _, imgfs := range b.tmpImgFsDirs {
		err = exterrors.Append(err, bundle.DeleteDirSafely(imgfs))
	}
	b.tmpImgFsDirs = nil
	err = errors.WithMessage(err, "close image builder")
	return
}

func (b *ImageBuilder) closeContainer() (err error) {
	if b.container != nil {
		err = b.container.Close()
		b.container = nil
	}
	return
}

func (b *ImageBuilder) resetBundle() (err error) {
	err = b.closeContainer()
	b.bundle = nil
	return
}

func (b *ImageBuilder) initBundle() (err error) {
	if b.bundle != nil {
		return
	}

	// Create locked bundle
	newBundle, err := b.bundles.CreateBundle("", false)
	if err != nil {
		return
	}
	b.bundle = newBundle
	b.lockedBundles = append(b.lockedBundles, newBundle)

	// Derive bundle spec from image
	builder := bundle.Builder(newBundle.ID())
	if b.image != nil {
		builder.SetImage(image.NewUnpackableImage(b.image, b.images))
	}
	if b.rootless {
		builder.ToRootless()
	}
	if b.proot != "" {
		builder.SetPRootPath(b.proot)
	}
	// TODO: use separate default network when not in rootless mode
	builder.UseHostNetwork()
	builder.SetProcessTerminal(false)
	builder.SetLinuxSeccompDefault()
	return builder.Build(newBundle)
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

func (b *ImageBuilder) fs() (r *tree.FsBuilder, err error) {
	if b.fsBuilder == nil {
		var rootfs fs.FsNode
		if b.image == nil {
			rootfs = tree.NewFS()
		} else {
			rootfs, err = b.images.FS(b.image.ID())
			if err != nil {
				return nil, err
			}
		}
		b.fsBuilder = tree.NewFsBuilder(rootfs, fs.NewFSOptions(b.rootless))
	}
	r = b.fsBuilder
	var imgId *digest.Digest
	if b.image != nil {
		id := b.image.ID()
		imgId = &id
	}
	r.HttpHeaderCache(b.cache.HttpHeaderCache(imgId))
	return
}

func (b *ImageBuilder) Image() digest.Digest {
	if b.image == nil {
		panic("Image() called before any image built")
	}
	return b.image.ID()
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
	if imageName == "" {
		return errors.New("from: no base image name provided")
	}
	if err = b.closeContainer(); err != nil {
		return
	}
	// Map name to image/bundle rootfs for efficient subsequent copy operations
	if b.bundle != nil {
		spec, err := b.bundle.Spec()
		if err != nil {
			return err
		}
		if spec.Root != nil {
			rootfs := filepath.Join(b.bundle.Dir(), spec.Root.Path)
			imgFs := imageFs{rootfs, b.image.ID()}
			b.namedFs[imgFs.imageId.String()] = &imgFs
			for _, name := range b.buildNames {
				b.namedFs[name] = &imgFs
			}
			b.bundle = nil
		} else {
			b.loggers.Warn.Printf("build names %+v refer to an image without file system", b.buildNames)
		}
		b.buildNames = nil
	} else if b.image != nil {
		for _, name := range b.buildNames {
			b.namedImages[name] = b.image
		}
	}
	b.buildNames = nil
	if err = b.resetBundle(); err != nil {
		return
	}
	b.loggers.Info.Println("FROM", imageName)
	var imgp *image.Image
	if imageName != "scratch" {
		img, err := b.imageResolver(b.images, imageName)
		if err != nil {
			return err
		}
		imgp = &img
	}
	b.fsBuilder = nil
	b.setImage(imgp)
	return
}

func (b *ImageBuilder) SetAuthor(author string) error {
	b.config.Author = author
	return b.cached("AUTHOR "+author, b.commitConfig)
}

func (b *ImageBuilder) SetUser(user string) (err error) {
	usr := idutils.ParseUser(user)
	b.config.Config.User = usr.String()
	return b.cached("USER "+usr.String(), func(createdBy string) (err error) {
		if _, err = b.resolveUser(&usr); err == nil {
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
	dir = filepath.Clean(b.absImagePath(dir))
	b.config.Config.WorkingDir = dir
	return b.cached("WORKDIR "+dir, b.commitConfig)
}

func (b *ImageBuilder) absImagePath(path string) string {
	origPath := path
	if !filepath.IsAbs(path) {
		path = filepath.Join(b.config.Config.WorkingDir, path)
	}
	path = filepath.Clean(path)
	if strings.HasSuffix(origPath, string(filepath.Separator)) {
		path += string(filepath.Separator)
	}
	return path
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

func (b *ImageBuilder) setImage(img *image.Image) {
	b.image = img
	if img == nil {
		b.config = ispecs.Image{}
		b.initConfig()
	} else {
		b.config = img.Config
	}
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

		p, err := b.container.Exec(proc, b.stdio)
		if err != nil {
			return
		}
		defer func() {
			if e := p.Close(); e != nil && err == nil {
				err = e
			}
		}()
		if err = p.Wait(); err != nil {
			return
		}
		rootfs := filepath.Join(b.bundle.Dir(), spec.Root.Path)
		fsNode, err := tree.FromDir(rootfs, b.rootless)
		if err != nil {
			return
		}
		return b.addLayer(fsNode, createdBy)
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
	if b.proot != "" {
		args = append([]string{"/dev/proot/proot", "-0"}, args...)
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
		b.image.Tag = img.Tag
		b.namedImages[tag] = b.image
		b.loggers.Info.WithField("img", b.image.ID()).WithField("tag", tag).Println("Tagged image")
	}
	return
}

func (b *ImageBuilder) AddFiles(buildDir string, srcPattern []string, dest string, user *idutils.User) (err error) {
	return b.addFiles(buildDir, srcPattern, dest, user, "ADD", opAdd)
}

func (b *ImageBuilder) CopyFiles(buildDir string, srcPattern []string, dest string, user *idutils.User) (err error) {
	return b.addFiles(buildDir, srcPattern, dest, user, "COPY", opCopy)
}

type addOp func(fs *tree.FsBuilder, buildDir string, srcPattern []string, dest string, usr *idutils.UserIds)

func opAdd(fs *tree.FsBuilder, buildDir string, srcPattern []string, dest string, usr *idutils.UserIds) {
	fs.AddAll(buildDir, srcPattern, dest, usr)
}

func opCopy(fs *tree.FsBuilder, buildDir string, srcPattern []string, dest string, usr *idutils.UserIds) {
	fs.CopyAll(buildDir, srcPattern, dest, usr)
}

func (b *ImageBuilder) addFiles(ctxDir string, srcPattern []string, dest string, user *idutils.User, opName string, modifyfs addOp) (err error) {
	dest = b.absImagePath(dest)
	defer exterrors.Wrapd(&err, "add files")
	if len(srcPattern) == 0 {
		return
	}
	fsBuilder, err := b.fs()
	if err != nil {
		return
	}
	usr, err := b.resolveUser(user)
	if err != nil {
		return
	}
	modifyfs(fsBuilder, ctxDir, srcPattern, dest, usr)
	imagefs, err := fsBuilder.FS()
	if err != nil {
		return
	}
	hash, err := imagefs.Hash(fs.AttrsHash)
	if err != nil {
		return
	}
	createdBy := fmt.Sprintf("%s --chown=%v %s", opName, user, hash)
	return b.cached(createdBy, func(createdBy string) (err error) {
		return b.addLayerAndUpdateBundleFS(imagefs, createdBy)
	})
}

func (b *ImageBuilder) CopyFilesFromImage(srcImage string, srcPattern []string, dest string, user *idutils.User) (err error) {
	dest = b.absImagePath(dest)
	defer exterrors.Wrapd(&err, "add files from image")

	s := make([]string, len(srcPattern))
	for i, e := range srcPattern {
		s[i] = strconv.Quote(e)
	}
	var imageId digest.Digest
	var cacheablefn func(createdBy string) error
	if fs := b.namedFs[srcImage]; fs != nil {
		// Copy from previous build's temp file system
		imageId = fs.imageId
		cacheablefn = func(createdBy string) error {
			return b.addFiles(fs.rootfs, srcPattern, dest, user, "COPY", opCopy)
		}
	} else {
		// Copy from image
		img, ok := b.namedImages[srcImage]
		if !ok {
			resolvedImg, e := b.imageResolver(b.images, srcImage)
			if e != nil {
				return e
			}
			img = &resolvedImg
		}
		imageId = img.ID()
		cacheablefn = func(createdBy string) (err error) {
			// Unpack image in temp dir
			err = os.MkdirAll(b.tempDir, 0750)
			if err != nil {
				return errors.New(err.Error())
			}
			imgRootfs, err := ioutil.TempDir(b.tempDir, ".tmp-imgfs-")
			if err != nil {
				return errors.New(err.Error())
			}
			// Store this image's fs in named fs map to be able to reuse it
			// (fs is deleted when builder is closed)
			b.tmpImgFsDirs = append(b.tmpImgFsDirs, imgRootfs)
			imgRootfs = filepath.Join(imgRootfs, "rootfs")
			imgFs := imageFs{imgRootfs, imageId}
			b.namedFs[srcImage] = &imgFs
			b.namedFs[imageId.String()] = &imgFs
			if err = b.images.UnpackImageLayers(img.ID(), imgRootfs); err != nil {
				return
			}

			// Resolve user
			usr, err := b.resolveUser(user)
			if err != nil {
				return
			}

			// Add files
			fsBuilder, err := b.fs()
			if err != nil {
				return
			}
			fsBuilder.CopyAll(imgRootfs, srcPattern, dest, usr)
			imagefs, err := fsBuilder.FS()
			if err != nil {
				return
			}
			return b.addLayerAndUpdateBundleFS(imagefs, createdBy)
		}
	}
	createdBy := fmt.Sprintf("COPY --from=%s --chown=%q %s %q", imageId, user, strings.Join(s, ", "), dest)
	return b.cached(createdBy, cacheablefn)
}

func (b *ImageBuilder) addLayer(imagefs fs.FsNode, createdBy string) (err error) {
	var parentImgId *digest.Digest
	if b.image != nil {
		pImgId := b.image.ID()
		parentImgId = &pImgId
	}
	img, err := b.images.AddLayer(imagefs, parentImgId, b.config.Author, createdBy)
	if err != nil {
		return
	}
	b.setImage(&img)
	b.fsBuilder = tree.NewFsBuilder(imagefs, fs.NewFSOptions(b.rootless))
	if b.bundle != nil {
		imgId := img.ID()
		err = b.bundle.SetParentImageId(&imgId)
	}
	return
}

func (b *ImageBuilder) addLayerAndUpdateBundleFS(imagefs fs.FsNode, createdBy string) (err error) {
	if b.bundle != nil {
		// Write files into bundle's rootfs as well if exists
		var bspec *rspecs.Spec
		if bspec, err = b.bundle.Spec(); err != nil {
			return errors.Wrap(err, "update bundle with new layer contents")
		}
		if bspec.Root != nil {
			bundlefs := filepath.Join(b.bundle.Dir(), bspec.Root.Path)
			dirWriter := writer.NewDirWriter(bundlefs, fs.NewFSOptions(b.rootless), b.loggers.Warn)
			if err = imagefs.Write(dirWriter); err == nil {
				err = dirWriter.Close()
			}
			if err != nil {
				err = exterrors.Append(err, b.resetBundle())
			}
		}
	}
	if err = b.addLayer(imagefs, createdBy); err != nil {
		return
	}
	return
}

func (b *ImageBuilder) resolveUser(u *idutils.User) (usrp *idutils.UserIds, err error) {
	if u == nil {
		return &idutils.UserIds{}, nil
	}
	user, err := u.ToIds()
	if err == nil {
		return &user, nil
	}

	// TODO: better resolve user using bundle's rootfs only when available, otherwise image's rootfs
	if err = b.initBundle(); err != nil {
		return &user, errors.Wrap(err, "resolve user name")
	}
	s, _ := b.bundle.Spec()
	if s.Root != nil {
		rootfs := filepath.Join(b.bundle.Dir(), s.Root.Path)
		user, err = u.Resolve(rootfs)
	} else {
		err = errors.New("no rootfs available to resolve user/group name from")
	}
	return &user, err
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
		b.setImage(&img)
		newImageId := img.ID()
		if b.bundle != nil {
			err = b.bundle.SetParentImageId(&newImageId)
		}
	}
	return errors.Wrap(err, "commit config")
}

func (b *ImageBuilder) cached(uniqCreatedBy string, call func(createdBy string) error) (err error) {
	b.loggers.Info.Println(uniqCreatedBy)

	defer func() {
		if err != nil {
			// Release bundle when operation failed to make sure it is not reused
			err = exterrors.Append(err, b.resetBundle())
		}
	}()

	var parentImgId *digest.Digest
	if b.image != nil {
		pImgId := b.image.ID()
		parentImgId = &pImgId
	}
	var cachedImgId digest.Digest
	cachedImgId, err = b.cache.GetCachedImageId(parentImgId, uniqCreatedBy)
	if err == nil {
		cachedImg, e := b.images.Image(cachedImgId)
		if e == nil {
			b.loggers.Info.WithField("img", cachedImg.ID()).Printf("Using cached image")
			b.setImage(&cachedImg)
			b.fsBuilder = nil
			return
		} else if !image.IsNotExist(e) {
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

	err = b.cache.PutCachedImageId(parentImgId, uniqCreatedBy, b.image.ID())

	b.loggers.Info.WithField("img", (*b.image).ID()).Printf("Built new image")

	return
}
