package store

import (
	"encoding/json"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/containers/image/copy"
	ocitransport "github.com/containers/image/oci/layout"
	"github.com/containers/image/transports/alltransports"
	"github.com/containers/image/types"
	"github.com/mgoltzsche/cntnr/image"
	exterrors "github.com/mgoltzsche/cntnr/pkg/errors"
	"github.com/mgoltzsche/cntnr/pkg/lock"
	"github.com/mgoltzsche/cntnr/pkg/log"
	digest "github.com/opencontainers/go-digest"
	ispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
)

const AnnotationParentImage = "com.github.mgoltzsche.cntnr.image.parent"

var _ image.ImageStoreRW = &ImageStoreRW{}

type ImageStoreRW struct {
	*ImageStoreRO
	systemContext *types.SystemContext
	//trustPolicy        *signature.PolicyContext
	trustPolicy TrustPolicyContext
	lock        lock.Locker
	warn        log.Logger
}

func NewImageStoreRW(locker lock.Locker, roStore *ImageStoreRO, systemContext *types.SystemContext, trustPolicy TrustPolicyContext, warn log.Logger) (r *ImageStoreRW, err error) {
	if err = locker.Lock(); err != nil {
		err = errors.Wrap(err, "open read/write image store")
	}
	return &ImageStoreRW{roStore.WithNonAtomicAccess(), systemContext, trustPolicy, locker, warn}, err
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

// Creates a new image ref. Overwrites existing refs.
func (s *ImageStoreRW) TagImage(imageId digest.Digest, tag string) (img image.Image, err error) {
	defer exterrors.Wrapd(&err, "tag")

	if tag == "" {
		return img, errors.New("no tag provided")
	}
	imgId, err := s.imageIds.ImageID(imageId)
	if err != nil {
		return
	}
	manifestDigest := imgId.ManifestDigest
	repo, ref := normalizeImageName(tag)
	manifest, err := s.blobs.ImageManifest(manifestDigest)
	if err != nil {
		return
	}
	f, err := s.blobs.BlobFileInfo(manifestDigest)
	if err != nil {
		return
	}
	manifestDescriptor := ispecs.Descriptor{
		MediaType: ispecs.MediaTypeImageManifest,
		Digest:    manifestDigest,
		Size:      f.Size(),
		Annotations: map[string]string{
			ispecs.AnnotationRefName: ref,
		},
		Platform: &ispecs.Platform{
			Architecture: runtime.GOARCH,
			OS:           runtime.GOOS,
		},
	}

	// Create/update index.json
	if err = s.addImages(repo, []ispecs.Descriptor{manifestDescriptor}); err != nil {
		return
	}

	return image.NewImage(manifestDigest, repo, ref, time.Now(), time.Now(), manifest, nil, s), err
}

func (s *ImageStoreRW) UntagImage(tag string) (err error) {
	defer exterrors.Wrapd(&err, "untag")
	repo, ref := normalizeImageName(tag)
	err = s.updateImageIndex(repo, false, func(repo *ImageRepo) error {
		idx := &repo.index
		if ref == "" {
			idx.Manifests = nil
		} else {
			manifests := make([]ispecs.Descriptor, 0, len(idx.Manifests))
			deleted := false
			for _, m := range idx.Manifests {
				if ref == m.Annotations[ispecs.AnnotationRefName] {
					deleted = true
				} else {
					manifests = append(manifests, m)
				}
			}
			idx.Manifests = manifests
			if !deleted {
				return errors.Errorf("image repo %q has no ref %q", repo, ref)
			}
		}
		return nil
	})
	return
}

func (s *ImageStoreRW) NewLayerSource(rootfs string, fileFilter []string) (image.LayerSource, error) {
	return s.blobs.NewLayerSource(rootfs, fileFilter)
}

func (s *ImageStoreRW) AddImageLayer(src image.LayerSource, parentImageId *digest.Digest, author, comment string) (r image.Image, err error) {
	defer exterrors.Wrapd(&err, "commit image layer")

	var parentManifest *digest.Digest
	if parentImageId != nil {
		parent, err := s.imageIds.ImageID(*parentImageId)
		if err != nil {
			return r, errors.Wrap(err, "resolve base image")
		}
		parentManifest = &parent.ManifestDigest
	}
	c, err := s.blobs.CommitLayer(src.(*LayerSource), parentManifest, author, comment)
	if err != nil {
		return
	}
	if err = s.imageIds.Add(c.Manifest.Config.Digest, c.Descriptor.Digest); err != nil {
		return
	}
	return s.Image(c.Manifest.Config.Digest)
}

func (s *ImageStoreRW) ImportImage(src string) (img image.Image, err error) {
	defer exterrors.Wrapd(&err, "import")

	// Parse source
	srcRef, err := alltransports.ParseImageName(src)
	if err != nil {
		err = errors.Wrapf(err, "invalid source %q", src)
		return
	}

	// Create temp image directory
	name, ref := nameAndRef(srcRef)
	if err = os.MkdirAll(s.repoDir, 0775); err != nil {
		return img, errors.New(err.Error())
	}
	imgDir, err := ioutil.TempDir(s.repoDir, ".tmp-img-")
	if err != nil {
		return img, errors.New(err.Error())
	}
	defer os.RemoveAll(imgDir)
	imgBlobDir := filepath.Join(imgDir, "blobs")
	if err = os.MkdirAll(s.blobs.blobDir, 0775); err != nil {
		return img, errors.New(err.Error())
	}
	if err = os.Symlink(s.blobs.blobDir, imgBlobDir); err != nil {
		return img, errors.New(err.Error())
	}

	// Parse destination
	destRef, err := ocitransport.Transport.ParseReference(imgDir + ":" + ref)
	if err != nil {
		err = errors.Wrapf(err, "invalid destination %q", imgDir)
		return
	}

	// Copy image
	trustPolicy, err := s.trustPolicy.Policy()
	if err != nil {
		return
	}
	err = copy.Image(trustPolicy, destRef, srcRef, &copy.Options{
		RemoveSignatures: false,
		SignBy:           "",
		ReportWriter:     os.Stdout,
		SourceCtx:        s.systemContext,
		DestinationCtx:   &types.SystemContext{},
	})
	if err != nil {
		return
	}

	// Add manifests to store's index
	var idx ispecs.Index
	if err = imageIndex(imgDir, &idx); err != nil {
		return
	}
	for _, m := range idx.Manifests {
		m.Annotations[AnnotationImported] = "true"
	}
	if err = s.addImages(name, idx.Manifests); err != nil {
		return
	}
	return s.ImageByName(src)
}

func (s *ImageStoreRW) MarkUsedImage(id digest.Digest) error {
	return s.imageIds.MarkUsed(id)
}

func (s *ImageStoreRW) AddImageConfig(conf ispecs.Image, parentImageId *digest.Digest) (img image.Image, err error) {
	var manifest ispecs.Manifest
	if parentImageId == nil {
		manifest = ispecs.Manifest{}
		manifest.Versioned.SchemaVersion = 2

		if conf.Config.Labels != nil {
			delete(conf.Config.Labels, AnnotationParentImage)
		}
	} else {
		parent, err := s.Image(*parentImageId)
		if err != nil {
			return img, errors.Wrap(err, "parent image")
		}
		manifest = parent.Manifest

		if conf.Config.Labels == nil {
			conf.Config.Labels = map[string]string{AnnotationParentImage: (*parentImageId).String()}
		} else {
			conf.Config.Labels[AnnotationParentImage] = (*parentImageId).String()
		}
	}

	//now := time.Now()
	//conf.Created = &now
	conf.Architecture = runtime.GOARCH
	conf.OS = runtime.GOOS
	confRef, err := s.blobs.PutImageConfig(conf)
	if err != nil {
		return
	}
	manifest.Config = confRef
	return s.putImageManifest(manifest)
}

func (s *ImageStoreRW) putImageManifest(manifest ispecs.Manifest) (r image.Image, err error) {
	d, err := s.blobs.PutImageManifest(manifest)
	if err != nil {
		return
	}
	if err = s.imageIds.Add(manifest.Config.Digest, d.Digest); err != nil {
		return
	}
	return s.Image(manifest.Config.Digest)
}

// Adds manifests to an image repo. This is an atomic operation
func (s *ImageStoreRW) addImages(repoName string, manifestDescriptors []ispecs.Descriptor) (err error) {
	if len(manifestDescriptors) == 0 {
		return nil
	}
	for _, manifestDescriptor := range manifestDescriptors {
		if manifestDescriptor.Annotations[ispecs.AnnotationRefName] == "" {
			return errors.Errorf("no image ref defined in manifest descriptor (%s annotation)", ispecs.AnnotationRefName)
		}
		if manifestDescriptor.Digest == digest.Digest("") || manifestDescriptor.Size < 1 || manifestDescriptor.Platform.Architecture == "" || manifestDescriptor.Platform.OS == "" {
			str := ""
			if b, e := json.Marshal(&manifestDescriptor); e == nil {
				str = string(b)
			}
			return errors.Errorf("add image: incomplete manifest descriptor %s", str)
		}
		manifest, err := s.blobs.ImageManifest(manifestDescriptor.Digest)
		if err != nil {
			return errors.Wrap(err, "add image")
		}
		if err = s.imageIds.Add(manifest.Config.Digest, manifestDescriptor.Digest); err != nil {
			return errors.Wrap(err, "add image")
		}
	}

	return s.updateImageIndex(repoName, true, func(repo *ImageRepo) error {
		for _, ref := range manifestDescriptors {
			repo.AddRef(ref)
		}
		return nil
	})
}

func (s *ImageStoreRW) updateImageIndex(repoName string, create bool, transform func(*ImageRepo) error) (err error) {
	dir, err := s.name2dir(repoName)
	if err == nil {
		var repo *ImageRepo
		repo, err = OpenImageRepo(dir, s.blobs.blobDir, create)
		if err == nil {
			defer func() {
				err = exterrors.Append(err, repo.Close())
			}()
			err = transform(repo)
		}
	}
	return errors.Wrapf(err, "update image repo %q index", repoName)
}
