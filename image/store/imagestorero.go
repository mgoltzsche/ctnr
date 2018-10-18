package store

import (
	"encoding/base64"
	"io/ioutil"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/mgoltzsche/ctnr/image"
	exterrors "github.com/mgoltzsche/ctnr/pkg/errors"
	"github.com/mgoltzsche/ctnr/pkg/log"
	digest "github.com/opencontainers/go-digest"
	ispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
)

var _ image.ImageStoreRO = &ImageStoreRO{}

type ImageStoreRO struct {
	blobs    *OCIBlobStore
	imageIds ImageIdStore
	repoDir  string
	warn     log.Logger
}

func NewImageStoreRO(dir string, blobStore *OCIBlobStore, imageIds ImageIdStore, warn log.Logger) (r *ImageStoreRO) {
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
	fi, err := s.blobs.GetInfo(manifestDigest)
	if err != nil {
		return
	}
	modTime := fi.ModTime()
	accessTime := modTime
	if len(manifest.Layers) > 0 {
		// ATTENTION: Here the last layer's access time is used as image last used time.
		// This also affects child images with a changed configuration only.
		if fi, err = s.blobs.GetInfo(manifest.Layers[len(manifest.Layers)-1].Digest); err != nil {
			return
		}
		stu := fi.Sys().(*syscall.Stat_t)
		accessTime = time.Unix(int64(stu.Atim.Sec), int64(stu.Atim.Nsec))
	}
	return image.NewImageInfo(manifestDigest, manifest, nil, modTime, accessTime), nil
}

func (s *ImageStoreRO) imageFromManifestDigest(manifestDigest digest.Digest) (r image.Image, err error) {
	img, err := s.imageInfoFromManifestDigest(manifestDigest)
	if err != nil {
		return
	}
	cfg, err := s.ImageConfig(img.Manifest.Config.Digest)
	return image.NewImage(img, cfg), err
}

func (s *ImageStoreRO) ImageByName(nameRef string) (r image.Image, err error) {
	defer exterrors.Wrapdf(&err, "image tag %q", nameRef)
	tag := normalizeImageName(nameRef)
	dir, err := s.repo2dir(tag.Repo)
	if err != nil {
		return
	}
	repo, err := NewImageRepo(tag.Repo, dir)
	if err != nil {
		if image.IsNotExist(err) {
			err = image.ErrNotExist(errors.Errorf("image repo %q not found in local store", tag.Repo))
		}
		return
	}
	d, err := repo.Manifest(tag.Ref)
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
		return r, image.ErrNotExist(errors.Errorf("inconsistent image store state: image name %s resolved but image ID file missing", nameRef))
	}
	return
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
	repos, err := s.Repos()
	if err != nil {
		return
	}
	for _, repo := range repos {
		refs, e := repo.Manifests()
		if e != nil {
			err = exterrors.Append(err, e)
		}
		r = append(make([]*image.ImageInfo, 0, len(r)+len(refs)), r...)
		for _, d := range refs {
			if img := imgManifestMap[d.Digest]; img != nil {
				withTag := *img
				withTag.Tag = &image.TagName{repo.Name, d.Annotations[ispecs.AnnotationRefName]}
				r = append(r, &withTag)
			} else {
				s.warn.Printf("image ID file for manifest %s is missing", d.Digest)
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

func (s *ImageStoreRO) RetainRepo(repoName string, keep map[digest.Digest]bool, maxPerRepo int) (err error) {
	dir, err := s.repo2dir(repoName)
	if err != nil {
		return
	}
	repo, err := NewLockedImageRepo(repoName, dir, s.blobs.dir())
	if err != nil {
		return
	}
	repo.Retain(keep)
	repo.Limit(maxPerRepo)
	return repo.Close()
}

func (s *ImageStoreRO) Repos() (r []*ImageRepo, err error) {
	fl, e := ioutil.ReadDir(s.repoDir)
	if e != nil {
		if os.IsNotExist(e) {
			return
		} else {
			return nil, errors.Wrap(e, "image repos")
		}
	}
	r = make([]*ImageRepo, 0, len(fl))
	for _, f := range fl {
		if !f.IsDir() || f.Name()[0] == '.' {
			continue
		}
		repoName, e := s.dir2repo(f.Name())
		if e != nil {
			err = exterrors.Append(err, e)
			continue
		}
		repo, e := NewImageRepo(repoName, filepath.Join(s.repoDir, f.Name()))
		if e != nil {
			err = exterrors.Append(err, e)
			continue
		}
		r = append(r, repo)
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
