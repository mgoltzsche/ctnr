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
	"github.com/mgoltzsche/cntnr/oci/image"
	"github.com/mgoltzsche/cntnr/pkg/lock"
	digest "github.com/opencontainers/go-digest"
	ispecs "github.com/opencontainers/image-spec/specs-go/v1"
)

var _ image.ImageStoreRW = &ImageStoreRW{}

type ImageStoreRW struct {
	*ImageStoreRO
	systemContext *types.SystemContext
	trustPolicy   *signature.PolicyContext
	lock          lock.SharedLock
}

func NewImageStoreRW(roStore *ImageStoreRO, systemContext *types.SystemContext) (r *ImageStoreRW, err error) {
	trustPolicy, err := createTrustPolicyContext()
	if err != nil {
		return
	}
	lck, err := lock.NewSharedLock(filepath.Join(os.TempDir(), "cntnr", "lock"))
	return &ImageStoreRW{roStore, systemContext, trustPolicy, lck}, err
}

func (s *ImageStoreRW) Close() error {
	err := s.lock.Close()
	s.ImageStoreRO = nil
	return err
}

// Creates a new image. Overwrites existing refs.
func (s *ImageStoreRW) CreateImage(name string, manifestDigest digest.Digest) (img image.Image, err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("create image: %s", err)
		}
	}()

	if name == "" {
		return img, fmt.Errorf("no name provided")
	}
	name, ref := normalizeImageName(name)
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
	if err = s.addImages(name, []ispecs.Descriptor{manifestDescriptor}); err != nil {
		return
	}

	return image.NewImage(manifestDigest, name, ref, time.Now(), manifest, nil, s.blobs), err
}

func (s *ImageStoreRW) DeleteImage(name string) (err error) {
	name, ref := normalizeImageName(name)
	if _, e := os.Stat(s.name2dir(name)); os.IsNotExist(e) {
		return fmt.Errorf("image %q does not exist", name)
	}
	err = s.updateImageIndex(name, false, func(repo *ImageRepo) error {
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
				return fmt.Errorf("image %q has no ref %q", name, ref)
			}
		}
		return nil
	})
	if err != nil {
		err = fmt.Errorf("delete image: %s", err)
	}
	return err
}

func (s *ImageStoreRW) CommitImage(rootfs, name string, parentManifest *digest.Digest, author, comment string) (r image.Image, err error) {
	c, err := s.blobs.CommitLayer(rootfs, parentManifest, author, comment)
	if err != nil {
		return r, fmt.Errorf("commit image: %s", err)
	}
	if name != "" {
		if r, err = s.CreateImage(name, c.Descriptor.Digest); err != nil {
			err = fmt.Errorf("commit image: %s", err)
		}
		return
	}
	r = image.NewImage(c.Descriptor.Digest, "", "", time.Now(), c.Manifest, &c.Config, s.blobs)
	return
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
	imgDir, err := ioutil.TempDir(s.imageDir, "tmpimg-")
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
	now := time.Now()
	return os.Chtimes(s.blobs.blobFile(id), now, now)
}

func (s *ImageStoreRW) PutImageConfig(config ispecs.Image) (ispecs.Descriptor, error) {
	return s.blobs.PutImageConfig(config)
}

func (s *ImageStoreRW) PutImageManifest(manifest ispecs.Manifest) (ispecs.Descriptor, error) {
	return s.blobs.PutImageManifest(manifest)
}

// Adds manifests to an image using a file lock
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

// TODO: move GC outside this object or fix locking
func (s *ImageStoreRW) ImageGC() (err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("image gc: %s", err)
		}
	}()

	if err = s.lock.Lock(); err != nil {
		return
	}
	defer unlock(s.lock, &err)

	// Collect named transitive blobs to leave them untouched
	keep := map[digest.Digest]bool{}
	imgs, err := s.Images()
	if err != nil {
		return err
	}
	for _, img := range imgs {
		keep[img.Digest] = true
		keep[img.Manifest.Config.Digest] = true
		for _, l := range img.Manifest.Layers {
			keep[l.Digest] = true
		}
	}

	// Delete all but the named blobs
	return s.blobs.RetainBlobs(keep)
}

func unlock(lock lock.Locker, err *error) {
	if e := lock.Unlock(); e != nil {
		if *err == nil {
			*err = e
		} else {
			fmt.Fprint(os.Stderr, "Error: %s", e)
		}
	}
}
