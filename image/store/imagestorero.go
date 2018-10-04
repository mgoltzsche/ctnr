package store

import (
	"encoding/base64"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
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
	blobs    *BlobStoreOci
	imageIds ImageIdStore
	repoDir  string
	warn     log.Logger
}

func NewImageStoreRO(dir string, blobStore *BlobStoreOci, imageIds ImageIdStore, warn log.Logger) (r *ImageStoreRO) {
	return &ImageStoreRO{blobStore, imageIds, dir, warn}
}

func (s *ImageStoreRO) ImageConfig(id digest.Digest) (ispecs.Image, error) {
	return s.blobs.ImageConfig(id)
}

func (s *ImageStoreRO) UnpackImageLayers(imageId digest.Digest, rootfs string) (err error) {
	img, err := s.imageIds.Get(imageId)
	if err != nil {
		return errors.Wrap(err, "unpack image layers")
	}
	return s.blobs.UnpackLayers(img.ManifestDigest, rootfs)
}

func (s *ImageStoreRO) Image(id digest.Digest) (r image.Image, err error) {
	imgId, err := s.imageIds.Get(id)
	if err == nil {
		r, err = s.imageFromManifestDigest(imgId.ManifestDigest)
	}
	err = errors.Wrapf(err, "image %q", id)
	return
}

func (s *ImageStoreRO) imageInfoFromManifestDigest(manifestDigest digest.Digest) (r image.ImageInfo, err error) {
	defer exterrors.Wrapd(&err, "image from manifest digest")
	manifest, err := s.blobs.ImageManifest(manifestDigest)
	if err != nil {
		return
	}
	mf, err := s.blobs.BlobFileInfo(manifestDigest)
	if err != nil {
		return
	}
	cf, err := s.blobs.BlobFileInfo(manifest.Config.Digest)
	if err != nil {
		return
	}
	return image.NewImageInfo(manifestDigest, manifest, nil, mf.ModTime(), accessTime(cf)), nil
}

func (s *ImageStoreRO) imageFromManifestDigest(manifestDigest digest.Digest) (r image.Image, err error) {
	img, err := s.imageInfoFromManifestDigest(manifestDigest)
	if err != nil {
		return
	}
	cfg, err := s.ImageConfig(img.Manifest.Config.Digest)
	return image.NewImage(img, cfg), err
}

func accessTime(fi os.FileInfo) time.Time {
	stu := fi.Sys().(*syscall.Stat_t)
	return time.Unix(int64(stu.Atim.Sec), int64(stu.Atim.Nsec))
}

func (s *ImageStoreRO) ImageByName(nameRef string) (r image.Image, err error) {
	defer exterrors.Wrapdf(&err, "image tag %q", nameRef)
	tag := normalizeImageName(nameRef)
	dir, err := s.repo2dir(tag.Repo)
	if err != nil {
		return
	}
	var idx ispecs.Index
	if err = imageIndex(dir, &idx); err != nil {
		if os.IsNotExist(err) {
			err = image.ErrNotExist(errors.Errorf("image repo %q not found in local store", tag.Repo))
		}
		return
	}
	d, err := findManifestDigest(&idx, tag.Ref)
	if err != nil {
		return
	}
	if r, err = s.imageFromManifestDigest(d.Digest); err != nil {
		return
	}
	idFileExists, err := s.imageIds.Exists(r.ID())
	if err != nil {
		return
	}
	if !idFileExists {
		// Return ErrNotExist when inconsistency found - the caller then may reimport the image
		return r, image.ErrNotExist(errors.Errorf("inconsistent state: image tag %s resolved but image ID file missing", nameRef))
	}
	return
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
		err = errors.Errorf("image ref %q not found for architecture %s and OS %s", ref, runtime.GOARCH, runtime.GOOS)
	} else {
		err = errors.Errorf("image ref %q not found", ref)
	}
	return d, image.ErrNotExist(err)
}

func (s *ImageStoreRO) Images() (r []*image.ImageInfo, err error) {
	defer exterrors.Wrapd(&err, "images")

	// Read all image IDs
	imgIDs, err := s.imageIds.Entries()
	if err != nil {
		return
	}
	imgMap := map[digest.Digest]*image.ImageInfo{}
	imgManifestMap := map[digest.Digest]*image.ImageInfo{}
	for _, imgId := range imgIDs {
		img, e := s.imageInfoFromManifestDigest(imgId.ManifestDigest)
		if e == nil {
			imgMap[img.ID()] = &img
			imgManifestMap[img.ManifestDigest] = &img
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
	r = make([]*image.ImageInfo, 0, len(fl))
	var idx ispecs.Index
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
							img := imgManifestMap[d.Digest]
							if img == nil {
								e = errors.Errorf("image ID file for manifest %s is missing", d.Digest)
							} else {
								withTag := *img
								withTag.Tag = &image.TagName{name, ref}
								r = append(r, &withTag)
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
		r = append(r, img)
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
