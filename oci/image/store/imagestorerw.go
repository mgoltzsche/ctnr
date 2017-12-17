package store

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/containers/image/copy"
	ocitransport "github.com/containers/image/oci/layout"
	"github.com/containers/image/signature"
	"github.com/containers/image/transports/alltransports"
	"github.com/containers/image/types"
	"github.com/mgoltzsche/cntnr/log"
	"github.com/mgoltzsche/cntnr/oci/image"
	"github.com/mgoltzsche/cntnr/pkg/lock"
	digest "github.com/opencontainers/go-digest"
	ispecs "github.com/opencontainers/image-spec/specs-go/v1"
)

const AnnotationParentImage = "com.github.mgoltzsche.cntnr.image.parent"

var _ image.ImageStoreRW = &ImageStoreRW{}

type ImageStoreRW struct {
	*ImageStoreRO
	systemContext *types.SystemContext
	trustPolicy   *signature.PolicyContext
	lock          lock.Locker
	warn          log.Logger
}

func NewImageStoreRW(locker lock.Locker, roStore *ImageStoreRO, systemContext *types.SystemContext, warn log.Logger) (r *ImageStoreRW, err error) {
	trustPolicy, err := createTrustPolicyContext()
	if err != nil {
		return
	}
	if err = locker.Lock(); err != nil {
		err = fmt.Errorf("open image store: %s", err)
	}
	return &ImageStoreRW{roStore.WithNonAtomicAccess(), systemContext, trustPolicy, locker, warn}, err
}

func (s *ImageStoreRW) Close() (err error) {
	if s.ImageStoreRO == nil {
		return nil
	}
	if err = s.lock.Unlock(); err != nil {
		s.warn.Printf("close store: %s", err)
	}
	s.ImageStoreRO = nil
	return err
}

// Creates a new image ref. Overwrites existing refs.
func (s *ImageStoreRW) TagImage(imageId digest.Digest, tag string) (img image.Image, err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("tag image: %s", err)
		}
	}()

	if tag == "" {
		return img, fmt.Errorf("no tag provided")
	}
	imgId, err := s.imageIds.ImageID(imageId)
	if err != nil {
		return
	}
	manifestDigest := imgId.ManifestDigest
	tag, ref := normalizeImageName(tag)
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
	if err = s.addImages(tag, []ispecs.Descriptor{manifestDescriptor}); err != nil {
		return
	}

	return image.NewImage(manifestDigest, tag, ref, time.Now(), time.Now(), manifest, nil, s), err
}

func (s *ImageStoreRW) UntagImage(tag string) (err error) {
	tag, ref := normalizeImageName(tag)
	if _, e := os.Stat(s.name2dir(tag)); os.IsNotExist(e) {
		return fmt.Errorf("image %q does not exist", tag)
	}
	err = s.updateImageIndex(tag, false, func(repo *ImageRepo) error {
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
				return fmt.Errorf("image %q has no ref %q", tag, ref)
			}
		}
		return nil
	})
	if err != nil {
		err = fmt.Errorf("delete image: %s", err)
	}
	return err
}

func (s *ImageStoreRW) AddImageLayer(rootfs string, parentImageId *digest.Digest, author, comment string) (r image.Image, err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("commit image: %s", err)
		}
	}()

	var parentManifest *digest.Digest
	if parentImageId != nil {
		parent, err := s.imageIds.ImageID(*parentImageId)
		if err != nil {
			return r, fmt.Errorf("resolve base image: %s", err)
		}
		parentManifest = &parent.ManifestDigest
	}
	c, err := s.blobs.CommitLayer(rootfs, parentManifest, author, comment)
	if err != nil {
		return
	}
	if err = s.imageIds.Add(c.Manifest.Config.Digest, c.Descriptor.Digest); err != nil {
		return
	}
	return s.Image(c.Manifest.Config.Digest)
}

func (s *ImageStoreRW) ImportImage(src string) (img image.Image, err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("import: %s", err)
		}
	}()

	// Parse source
	srcRef, err := alltransports.ParseImageName(src)
	if err != nil {
		err = fmt.Errorf("invalid source %q: %s", src, err)
		return
	}

	// Create temp image directory
	name, ref := nameAndRef(srcRef)
	imgDir, err := ioutil.TempDir(s.repoDir, "tmpimg-")
	if err != nil {
		return
	}
	defer os.RemoveAll(imgDir)
	imgBlobDir := filepath.Join(imgDir, "blobs")
	if err = os.Symlink(s.blobs.blobDir, imgBlobDir); err != nil {
		return
	}

	// Parse destination
	destRef, err := ocitransport.Transport.ParseReference(imgDir + ":" + ref)
	if err != nil {
		err = fmt.Errorf("invalid destination %q: %s", imgDir, err)
		return
	}

	// Copy image
	err = copy.Image(s.trustPolicy, destRef, srcRef, &copy.Options{
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
			return img, fmt.Errorf("parent image: %s", err)
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
func (s *ImageStoreRW) addImages(name string, manifestDescriptors []ispecs.Descriptor) (err error) {
	if len(manifestDescriptors) == 0 {
		return nil
	}
	for _, manifestDescriptor := range manifestDescriptors {
		if manifestDescriptor.Annotations[ispecs.AnnotationRefName] == "" {
			return fmt.Errorf("no image ref defined in manifest descriptor (%s annotation)", ispecs.AnnotationRefName)
		}
		if manifestDescriptor.Digest == digest.Digest("") || manifestDescriptor.Size < 1 || manifestDescriptor.Platform.Architecture == "" || manifestDescriptor.Platform.OS == "" {
			str := ""
			if b, e := json.Marshal(&manifestDescriptor); e == nil {
				str = string(b)
			}
			return fmt.Errorf("incomplete manifest descriptor %s", str)
		}
		manifest, err := s.blobs.ImageManifest(manifestDescriptor.Digest)
		if err != nil {
			return fmt.Errorf("add images: %s", err)
		}
		if err = s.imageIds.Add(manifest.Config.Digest, manifestDescriptor.Digest); err != nil {
			return err
		}
	}

	return s.updateImageIndex(name, true, func(repo *ImageRepo) error {
		for _, ref := range manifestDescriptors {
			repo.AddRef(ref)
		}
		return nil
	})
}

func (s *ImageStoreRW) updateImageIndex(name string, create bool, transform func(*ImageRepo) error) (err error) {
	repo, err := OpenImageRepo(s.name2dir(name), s.blobs.blobDir, create)
	if err != nil {
		return fmt.Errorf("update image index: %s", err)
	}
	defer func() {
		if e := repo.Close(); e != nil {
			if err == nil {
				err = e
			} else {
				fmt.Fprintf(os.Stderr, "update image index: %s", e)
			}
		}
	}()

	return transform(repo)
}
