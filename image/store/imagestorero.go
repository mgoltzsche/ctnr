package store

import (
	"encoding/base64"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/mgoltzsche/cntnr/image"
	exterrors "github.com/mgoltzsche/cntnr/pkg/errors"
	"github.com/mgoltzsche/cntnr/pkg/log"
	digest "github.com/opencontainers/go-digest"
	ispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
)

var _ image.ImageStoreRO = &ImageStoreRO{}

type ImageStoreRO struct {
	blobs       *BlobStoreOci
	imageReader image.ImageReader
	imageIds    ImageIdStore
	repoDir     string
	warn        log.Logger
}

func NewImageStoreRO(dir string, blobStore *BlobStoreOci, imageIds ImageIdStore, warn log.Logger) (r *ImageStoreRO) {
	return &ImageStoreRO{blobStore, nil, imageIds, dir, warn}
}

func (s *ImageStoreRO) WithNonAtomicAccess() *ImageStoreRO {
	return &ImageStoreRO{s.blobs, s, s.imageIds, s.repoDir, s.warn}
}

func (s *ImageStoreRO) ImageConfig(id digest.Digest) (ispecs.Image, error) {
	return s.blobs.ImageConfig(id)
}

func (s *ImageStoreRO) UnpackImageLayers(imageId digest.Digest, rootfs string) (err error) {
	img, err := s.imageIds.ImageID(imageId)
	if err == nil {
		if err = s.imageIds.MarkUsed(imageId); err == nil {
			return s.blobs.UnpackLayers(img.ManifestDigest, rootfs)
		}
	}
	return errors.Wrap(err, "unpack image layers")
}

func (s *ImageStoreRO) Image(id digest.Digest) (r image.Image, err error) {
	imgId, err := s.imageIds.ImageID(id)
	if err == nil {
		r, err = s.imageFromManifestDigest(imgId.ManifestDigest, imgId.LastUsed)
	}
	err = errors.Wrapf(err, "image %q", id)
	return
}

func (s *ImageStoreRO) imageFromManifestDigest(manifestDigest digest.Digest, lastUsed time.Time) (r image.Image, err error) {
	defer exterrors.Wrapd(&err, "unpack image layers")
	manifest, err := s.blobs.ImageManifest(manifestDigest)
	if err != nil {
		return
	}
	f, err := s.blobs.BlobFileInfo(manifestDigest)
	if err != nil {
		return
	}
	return image.NewImage(manifestDigest, "", "", f.ModTime(), lastUsed, manifest, nil, s.imageReader), err
}

func (s *ImageStoreRO) ImageByName(nameRef string) (r image.Image, err error) {
	defer exterrors.Wrapdf(&err, "image tag %q", nameRef)
	repo, ref := normalizeImageName(nameRef)
	dir, err := s.repo2dir(repo)
	if err != nil {
		return
	}
	var idx ispecs.Index
	if err = imageIndex(dir, &idx); err != nil {
		if os.IsNotExist(err) {
			err = image.ErrorImageNameNotExist("image repo %q not found in local store", repo)
		}
		return
	}
	d, err := findManifestDigest(&idx, ref)
	if err != nil {
		return
	}
	return s.imageFromManifestDigest(d.Digest, time.Now())
}

func findManifestDigest(idx *ispecs.Index, ref string) (d ispecs.Descriptor, err error) {
	refFound := false
	for _, descriptor := range idx.Manifests {
		if descriptor.Annotations[ispecs.AnnotationRefName] == ref {
			refFound = true
			if descriptor.Platform.Architecture == runtime.GOARCH && descriptor.Platform.OS == runtime.GOOS {
				if descriptor.MediaType != ispecs.MediaTypeImageManifest {
					err = errors.Errorf("unsupported manifest media type %q", descriptor.MediaType)
				}
				return descriptor, err
			}
		}
	}
	if refFound {
		err = image.ErrorImageNameNotExist("image ref %q not found for architecture %s and OS %s", ref, runtime.GOARCH, runtime.GOOS)
	} else {
		err = image.ErrorImageNameNotExist("image ref %q not found", ref)
	}
	return
}

func (s *ImageStoreRO) Images() (r []image.Image, err error) {
	defer exterrors.Wrapd(&err, "images")

	// Read all image IDs
	imgIDs, err := s.imageIds.ImageIDs()
	if err != nil {
		return
	}
	imgMap := map[digest.Digest]*image.Image{}
	for _, imgId := range imgIDs {
		img, e := s.imageFromManifestDigest(imgId.ManifestDigest, imgId.LastUsed)
		if e == nil {
			img.LastUsed = imgId.LastUsed
			imgMap[img.ID()] = &img
		} else {
			err = e
		}
	}

	// Read image repos
	var fl []os.FileInfo
	if _, e := os.Stat(s.repoDir); e == nil || !os.IsNotExist(e) {
		if fl, err = ioutil.ReadDir(s.repoDir); err != nil {
			return
		}
	}
	r = make([]image.Image, 0, len(fl))
	var idx ispecs.Index
	var manifest ispecs.Manifest
	for _, f := range fl {
		if f.IsDir() && f.Name()[0] != '.' {
			name := f.Name()
			name, e := s.dir2repo(name)
			if e == nil {
				dir, e := s.repo2dir(name)
				if e != nil {
					s.warn.Println(e)
					continue
				}
				if e = imageIndex(dir, &idx); e == nil {
					for _, d := range idx.Manifests {
						ref := d.Annotations[ispecs.AnnotationRefName]
						if ref == "" {
							e = errors.Errorf("manifest descriptor %s of image %q has no ref", d.Digest, name)
						} else {
							manifest, e = s.blobs.ImageManifest(d.Digest)
							if e == nil {
								if img := imgMap[manifest.Config.Digest]; img != nil {
									r = append(r, image.NewImage(d.Digest, name, ref, img.Created, img.LastUsed, manifest, nil, s.imageReader))
								} else {
									e = errors.Errorf("image %s ID file is missing", manifest.Config.Digest)
								}
							}
						}
					}
				}
			}
			if e != nil {
				s.warn.Printf("image %q: %s", name, e)
			}
		}
	}

	// Add untagged images
	for _, img := range r {
		delete(imgMap, img.ID())
	}
	for _, img := range imgMap {
		r = append(r, *img)
	}
	return
}

func (s *ImageStoreRO) repo2dir(repo string) (string, error) {
	if repo == "" {
		return repo, errors.New("no repo name provided")
	}
	repo = base64.RawStdEncoding.EncodeToString([]byte(repo))
	return filepath.Join(s.repoDir, repo), nil
}

func (s *ImageStoreRO) dir2repo(fileName string) (repo string, err error) {
	b, err := base64.RawStdEncoding.DecodeString(fileName)
	repo = string(b)
	if err != nil {
		repo = fileName
		err = errors.Wrapf(err, "repo name from dir")
	}
	return
}
