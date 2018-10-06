package store

import (
	"context"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/containers/image/copy"
	ocitransport "github.com/containers/image/oci/layout"
	"github.com/containers/image/transports"
	"github.com/containers/image/transports/alltransports"
	"github.com/containers/image/types"
	"github.com/mgoltzsche/cntnr/image"
	exterrors "github.com/mgoltzsche/cntnr/pkg/errors"
	"github.com/mgoltzsche/cntnr/pkg/fs"
	"github.com/mgoltzsche/cntnr/pkg/lock"
	"github.com/mgoltzsche/cntnr/pkg/log"
	digest "github.com/opencontainers/go-digest"
	ispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
)

var _ image.ImageStoreRW = &ImageStoreRW{}

type ImageStoreRW struct {
	*ImageStoreRO
	systemContext *types.SystemContext
	//trustPolicy        *signature.PolicyContext
	trustPolicy TrustPolicyContext
	rootless    bool
	temp        string
	lock        lock.Locker
	loggers     log.Loggers
}

func NewImageStoreRW(locker lock.Locker, roStore *ImageStoreRO, tmpDir string, systemContext *types.SystemContext, trustPolicy TrustPolicyContext, rootless bool, loggers log.Loggers) (r *ImageStoreRW, err error) {
	if err = locker.Lock(); err != nil {
		err = errors.Wrap(err, "open read/write image store")
	}
	return &ImageStoreRW{roStore, systemContext, trustPolicy, rootless, tmpDir, locker, loggers}, err
}

func (s *ImageStoreRW) Close() (err error) {
	if s.ImageStoreRO == nil {
		return nil
	}
	if err = s.lock.Unlock(); err != nil {
		err = errors.New("close store: " + err.Error())
		s.warn.Println(err)
	}
	s.ImageStoreRO = nil
	return err
}

func (s *ImageStoreRW) SupportsTransport(transportName string) bool {
	return transports.Get(transportName) != nil
}

func (s *ImageStoreRW) ImportImage(src string) (img image.Image, err error) {
	defer exterrors.Wrapd(&err, "import")

	// Parse source
	srcRef, err := alltransports.ParseImageName(src)
	if err != nil {
		err = errors.WithMessage(err, "source")
		return
	}

	// Create temp image directory
	tag := nameAndRef(srcRef)
	if err = os.MkdirAll(s.repoDir, 0775); err != nil {
		return img, errors.New(err.Error())
	}
	imgDir, err := ioutil.TempDir(s.repoDir, ".tmp-img-")
	if err != nil {
		return img, errors.New(err.Error())
	}
	defer os.RemoveAll(imgDir)
	imgBlobDir := filepath.Join(imgDir, "blobs")
	extBlobDir := s.blobs.dir()
	if err = os.MkdirAll(extBlobDir, 0775); err != nil {
		return img, errors.New(err.Error())
	}
	if err = os.Symlink(extBlobDir, imgBlobDir); err != nil {
		return img, errors.New(err.Error())
	}

	// Parse destination
	destRef, err := ocitransport.Transport.ParseReference(imgDir + ":" + tag.Ref)
	if err != nil {
		err = errors.Wrapf(err, "invalid destination %q", imgDir)
		return
	}

	// Copy image
	trustPolicy, err := s.trustPolicy.Policy()
	if err != nil {
		return
	}
	err = copy.Image(context.Background(), trustPolicy, destRef, srcRef, &copy.Options{
		RemoveSignatures: false,
		SignBy:           "",
		ReportWriter:     os.Stdout,
		SourceCtx:        s.systemContext,
		DestinationCtx:   &types.SystemContext{},
	})
	if err != nil {
		return
	}

	// Read downloaded image index
	tmpRepo, err := NewImageRepo(tag.Repo, imgDir)
	if err != nil {
		return
	}
	dir, err := s.repo2dir(tag.Repo)
	if err != nil {
		return
	}
	manifests, err := tmpRepo.Manifests()
	if err != nil {
		s.warn.Println(err)
	}

	// Map image IDs to manifests
	for _, m := range manifests {
		manifest, e := s.blobs.ImageManifest(m.Digest)
		if e != nil {
			return img, e
		}
		if err = s.imageIds.Put(manifest.Config.Digest, m.Digest); err != nil {
			return
		}
	}

	// Add manifests (with ref) to store's index
	repo, err := NewLockedImageRepo(tag.Repo, dir, s.blobs.dir())
	if err != nil {
		return
	}
	for _, m := range manifests {
		m.Annotations[AnnotationImported] = "true"
		repo.AddManifest(m)
	}
	if err = repo.Close(); err != nil {
		return
	}
	return s.ImageByName(src)
}

// Returns the image's fs spec (files not extractable)
func (s *ImageStoreRW) FS(imageId digest.Digest) (r fs.FsNode, err error) {
	imgId, err := s.imageIds.Get(imageId)
	if err != nil {
		return nil, errors.Wrap(err, "load image fs spec: resolve image ID")
	}
	return s.blobs.FSSpec(imgId.ManifestDigest)
}

func (s *ImageStoreRW) AddLayer(rootfs fs.FsNode, parentImageId *digest.Digest, author, createdByOp string) (img image.Image, err error) {
	var parentManifestId *digest.Digest
	if parentImageId != nil {
		pImgId, err := s.imageIds.Get(*parentImageId)
		if err != nil {
			return img, errors.Wrap(err, "add image layer: resolve parent image ID")
		}
		parentManifestId = &pImgId.ManifestDigest
	}
	c, err := s.blobs.AddLayer(rootfs, parentManifestId, author, createdByOp)
	exists := image.IsEmptyLayerDiff(err)
	if err != nil && !exists {
		return
	}
	if exists {
		return s.Image(*parentImageId)
	}
	if err = s.imageIds.Put(c.Manifest.Config.Digest, c.Descriptor.Digest); err != nil {
		return img, errors.WithMessage(err, "add image layer")
	}
	now := time.Now()
	return image.NewImage(image.NewImageInfo(c.Descriptor.Digest, c.Manifest, nil, now, now), c.Config), nil
}

func (s *ImageStoreRW) AddImageConfig(conf ispecs.Image, parentImageId *digest.Digest) (img image.Image, err error) {
	// Lookup parent manifest digest and set image id annotation
	var parentManifest *digest.Digest
	if parentImageId == nil {
		if conf.Config.Labels != nil {
			delete(conf.Config.Labels, AnnotationParentManifest)
		}
	} else {
		pImg, err := s.imageIds.Get(*parentImageId)
		if err != nil {
			return img, errors.WithMessage(err, "add image config: resolve parent manifest")
		}
		parentManifest = &pImg.ManifestDigest

		if conf.Config.Labels == nil {
			conf.Config.Labels = map[string]string{}
		}
		conf.Config.Labels[AnnotationParentManifest] = (*parentImageId).String()
	}

	// Write image config and new manifest
	manifestRef, manifest, err := s.blobs.PutImageConfig(conf, parentManifest)
	if err == nil {
		// Map imageID (config digest) to manifest
		if err = s.imageIds.Put(manifest.Config.Digest, manifestRef.Digest); err == nil {
			now := time.Now()
			img = image.NewImage(image.NewImageInfo(manifestRef.Digest, manifest, nil, now, now), conf)
		}
	}
	err = errors.WithMessage(err, "add image config")
	return
}

// Creates a new image ref. Overwrites existing refs.
func (s *ImageStoreRW) TagImage(imageId digest.Digest, tagStr string) (img image.ImageInfo, err error) {
	defer exterrors.Wrapd(&err, "tag")

	if tagStr == "" {
		return img, errors.New("no tag provided")
	}
	imgId, err := s.imageIds.Get(imageId)
	if err != nil {
		return
	}
	manifestDigest := imgId.ManifestDigest
	tag := normalizeImageName(tagStr)
	manifest, err := s.blobs.ImageManifest(manifestDigest)
	if err != nil {
		return
	}
	f, err := s.blobs.GetInfo(manifestDigest)
	if err != nil {
		return
	}
	manifestDescriptor := ispecs.Descriptor{
		MediaType: ispecs.MediaTypeImageManifest,
		Digest:    manifestDigest,
		Size:      f.Size(),
		Annotations: map[string]string{
			ispecs.AnnotationRefName: tag.Ref,
		},
		Platform: &ispecs.Platform{
			Architecture: runtime.GOARCH,
			OS:           runtime.GOOS,
		},
	}

	// Create/update index.json
	dir, err := s.repo2dir(tag.Repo)
	if err != nil {
		return
	}
	repo, err := NewLockedImageRepo(tag.Repo, dir, s.blobs.dir())
	if err != nil {
		return
	}
	defer func() {
		err = repo.Close()
	}()
	repo.AddManifest(manifestDescriptor)
	return image.NewImageInfo(manifestDigest, manifest, tag, f.ModTime(), f.ModTime()), err
}

func (s *ImageStoreRW) UntagImage(tagStr string) (err error) {
	defer exterrors.Wrapd(&err, "untag")
	tag := normalizeImageName(tagStr)
	dir, err := s.repo2dir(tag.Repo)
	if err != nil {
		return
	}
	repo, err := NewLockedImageRepo(tag.Repo, dir, s.blobs.dir())
	if err != nil {
		return
	}
	defer func() {
		err = repo.Close()
	}()
	repo.DelManifest(tag.Ref)
	return
}
