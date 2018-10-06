package store

import (
	"time"

	"github.com/mgoltzsche/cntnr/image"
	exterrors "github.com/mgoltzsche/cntnr/pkg/errors"
	"github.com/opencontainers/go-digest"
)

const (
	AnnotationImported       = "com.github.mgoltzsche.cntnr.image.imported"
	AnnotationParentManifest = "com.github.mgoltzsche.cntnr.image.parent.manifest"
)

type imageGC struct {
	TTL         time.Duration
	RefTTL      time.Duration
	MaxPerRepo  int
	store       *ImageStoreRW
	manifestMap map[digest.Digest]*image.ImageInfo
	keepBlobs   map[digest.Digest]bool
	keepImgIds  map[digest.Digest]bool
	keepFsSpecs map[digest.Digest]bool
}

func newImageGC(store *ImageStoreRW, ttl, refTTL time.Duration, maxPerRepo int) *imageGC {
	return &imageGC{
		TTL:         ttl,
		RefTTL:      refTTL,
		MaxPerRepo:  maxPerRepo,
		store:       store,
		manifestMap: map[digest.Digest]*image.ImageInfo{},
		keepBlobs:   map[digest.Digest]bool{},
		keepImgIds:  map[digest.Digest]bool{},
		keepFsSpecs: map[digest.Digest]bool{},
	}
}

func (s *imageGC) GC() (err error) {
	before := time.Now().Add(-s.TTL)
	defer exterrors.Wrapd(&err, "image gc")

	// Map all image IDs
	imgs, err := s.store.Images()
	if err != nil {
		return
	}
	for _, img := range imgs {
		s.manifestMap[img.ManifestDigest] = img
	}

	// Preserve most recently used images
	for _, img := range imgs {
		if img.LastUsed.After(before) {
			s.keep(img)
		}
	}

	// Preserve (most recently used) tags
	repos, err := s.store.Repos()
	if err != nil {
		return
	}
	before = time.Now().Add(-s.RefTTL)
	for _, repo := range repos {
		manifests, e := repo.Manifests()
		err = exterrors.Append(err, e)
		containsExpired := false
		for _, manifest := range manifests {
			img := s.manifestMap[manifest.Digest]
			if img != nil && (s.RefTTL <= 0 || img.LastUsed.After(before)) {
				s.keep(img)
				continue
			}
			containsExpired = true
		}
		if containsExpired || len(manifests) > s.MaxPerRepo {
			// TODO: avoid lock/reload here since this is run within exclusive lock anyway
			s.store.RetainRepo(repo.Name, s.keepBlobs, s.MaxPerRepo)
		}
	}

	// Delete everything but the marked fsspecs, imageids, blobs
	err = exterrors.Append(err, s.store.imageIds.Retain(s.keepImgIds))
	err = exterrors.Append(err, s.store.blobs.Retain(s.keepBlobs))
	err = exterrors.Append(err, s.store.blobs.fsspecs.Retain(s.keepFsSpecs))
	return
}

func (s *imageGC) keep(img *image.ImageInfo) {
	s.keepImgIds[img.ID()] = true
	s.keepBlobs[img.ID()] = true
	s.keepBlobs[img.ManifestDigest] = true
	for _, l := range img.Manifest.Layers {
		s.keepBlobs[l.Digest] = true
	}
	if conf, e := s.store.ImageConfig(img.Manifest.Config.Digest); e == nil {
		s.keepFsSpecs[chainID(conf.RootFS.DiffIDs)] = true
	}
	if img.Manifest.Annotations != nil {
		if parentManifestId := img.Manifest.Annotations[AnnotationParentManifest]; parentManifestId != "" {
			if parentManifestDigest, e := digest.Parse(parentManifestId); e == nil {
				if parentImg := s.manifestMap[parentManifestDigest]; parentImg != nil {
					if kept := s.keepImgIds[parentImg.ID()]; !kept {
						s.keep(parentImg)
					}
				}
			}
		}
	}
}
