package store

import (
	"encoding/base64"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"time"

	"github.com/mgoltzsche/cntnr/log"
	"github.com/mgoltzsche/cntnr/oci/image"
	digest "github.com/opencontainers/go-digest"
	ispecs "github.com/opencontainers/image-spec/specs-go/v1"
)

var _ image.ImageStoreRO = &ImageStoreRO{}

type ImageStoreRO struct {
	blobs       *BlobStoreExt
	imageReader image.ImageReader
	imageIds    ImageIdStore
	repoDir     string
	warn        log.Logger
}

func NewImageStoreRO(dir string, blobStore *BlobStoreExt, imageIds ImageIdStore, warn log.Logger) (r *ImageStoreRO) {
	return &ImageStoreRO{blobStore, nil, imageIds, dir, warn}
}

func (s *ImageStoreRO) WithNonAtomicAccess() *ImageStoreRO {
	return &ImageStoreRO{s.blobs, s, s.imageIds, s.repoDir, s.warn}
}

func (s *ImageStoreRO) ImageConfig(id digest.Digest) (ispecs.Image, error) {
	return s.blobs.ImageConfig(id)
}

func (s *ImageStoreRO) UnpackImageLayers(imageId digest.Digest, rootfs string) error {
	img, err := s.imageIds.ImageID(imageId)
	if err != nil {
		return fmt.Errorf("unpack image layers: %s", err)
	}
	if err = s.imageIds.MarkUsed(imageId); err != nil {
		return fmt.Errorf("unpack image layers: %s", err)
	}
	return s.blobs.UnpackLayers(img.ManifestDigest, rootfs)
}

func (s *ImageStoreRO) Image(id digest.Digest) (r image.Image, err error) {
	imgId, err := s.imageIds.ImageID(id)
	if err != nil {
		return r, fmt.Errorf("image: %s", err)
	}
	return s.imageFromManifestDigest(imgId.ManifestDigest, imgId.LastUsed)
}

func (s *ImageStoreRO) imageFromManifestDigest(manifestDigest digest.Digest, lastUsed time.Time) (r image.Image, err error) {
	manifest, err := s.blobs.ImageManifest(manifestDigest)
	if err != nil {
		return r, fmt.Errorf("image: %s", err)
	}
	f, err := s.blobs.BlobFileInfo(manifestDigest)
	if err != nil {
		return r, fmt.Errorf("image: %s", err)
	}
	return image.NewImage(manifestDigest, "", "", f.ModTime(), lastUsed, manifest, nil, s), err
}

func (s *ImageStoreRO) ImageByName(nameRef string) (r image.Image, err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("image %q not found in local store: %s", nameRef, err)
		}
	}()
	if id, e := digest.Parse(nameRef); e == nil && id.Validate() == nil {
		return s.Image(id)
	}
	name, ref := normalizeImageName(nameRef)
	var idx ispecs.Index
	if err = imageIndex(s.name2dir(name), &idx); err != nil {
		return
	}
	d, err := findManifestDigest(&idx, ref)
	if err != nil {
		return
	}
	return s.imageFromManifestDigest(d.Digest, time.Now())
}

func (s *ImageStoreRO) Images() (r []image.Image, err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("images: %s", err)
		}
	}()

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
	fl, err := ioutil.ReadDir(s.repoDir)
	if err != nil {
		return
	}
	r = make([]image.Image, 0, len(fl))
	var idx ispecs.Index
	var manifest ispecs.Manifest
	for _, f := range fl {
		if f.IsDir() {
			name := f.Name()
			name, e := s.dir2name(name)
			if e == nil {
				if e = imageIndex(s.name2dir(name), &idx); e == nil {
					for _, d := range idx.Manifests {
						ref := d.Annotations[ispecs.AnnotationRefName]
						if ref == "" {
							e = fmt.Errorf("image %q contains manifest descriptor without ref", name, d.Digest)
						} else {
							manifest, e = s.blobs.ImageManifest(d.Digest)
							if e == nil {
								if img := imgMap[manifest.Config.Digest]; img != nil {
									r = append(r, image.NewImage(d.Digest, name, ref, img.Created, img.LastUsed, manifest, nil, s.imageReader))
								} else {
									e = fmt.Errorf("image %s ID file is missing", manifest.Config.Digest)
								}
							}
						}
					}
				}
			}
			if e != nil {
				s.warn.Printf("image %q: %s", name, e)
				if err == nil {
					err = e
				}
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

func (s *ImageStoreRO) name2dir(name string) string {
	name = base64.RawStdEncoding.EncodeToString([]byte(name))
	return filepath.Join(s.repoDir, name)
}

func (s *ImageStoreRO) dir2name(fileName string) (name string, err error) {
	b, err := base64.RawStdEncoding.DecodeString(fileName)
	if err == nil {
		name = string(b)
	} else {
		name = fileName
		err = fmt.Errorf("image name: %s", err)
	}
	return
}
