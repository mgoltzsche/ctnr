package store

import (
	"fmt"
	"path/filepath"

	"github.com/containers/image/types"
	"github.com/mgoltzsche/cntnr/log"
	"github.com/mgoltzsche/cntnr/oci/bundle"
	bstore "github.com/mgoltzsche/cntnr/oci/bundle/store"
	"github.com/mgoltzsche/cntnr/oci/image"
	"github.com/mgoltzsche/cntnr/oci/image/builder"
	istore "github.com/mgoltzsche/cntnr/oci/image/store"
	"github.com/openSUSE/umoci/pkg/fseval"
)

// TODO: Make sure store is closed before running any container to free shared lock to allow other container to be prepared
// TODO: base Commit method in BlobStore (so that mtree can move to blobstore), UnpackImage method in ImageStore

// Minimal Store interface.
// containers/storage interface is not used to ease the OCI store implementation
// which is required by unprivileged users (https://github.com/containers/storage/issues/96)

var _ image.ImageStore = &Store{}

type Store struct {
	image.ImageStore
	bundle.BundleStore
}

func (s *Store) ImageBuilder(baseImageId string, newContainerId, author string) (b *builder.ImageBuilder, err error) {
	rwstore, err := s.OpenImageRWStore()
	if err != nil {
		return
	}
	defer func() {
		if err != nil {
			err = fmt.Errorf("image builder: %s", err)
			rwstore.Close()
		}
	}()

	baseImage, err := rwstore.ImageByName(baseImageId)
	if err != nil {
		return
	}

	return builder.NewImageBuilder(rwstore, s.BundleStore, baseImage, newContainerId, author)
}

func (s *Store) ImageBuilderFromBundle(container bundle.Bundle, author string) (b *builder.ImageBuilder, err error) {
	rwstore, err := s.OpenImageRWStore()
	if err != nil {
		return
	}
	defer func() {
		if err != nil {
			err = fmt.Errorf("image builder from bundle: %s", err)
			rwstore.Close()
		}
	}()

	return builder.NewImageBuilderFromBundle(rwstore, container, author)
}

func OpenStore(dir string, rootless bool, systemContext *types.SystemContext, errorLog log.Logger, debugLog log.Logger) (r *Store, err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("init store: %s", err)
		}
	}()
	dir, err = filepath.Abs(dir)
	if err != nil {
		return
	}
	blobDir := filepath.Join(dir, "blobs")
	mtreeDir := filepath.Join(dir, "mtree")
	imageDir := filepath.Join(dir, "images")
	bundleDir := filepath.Join(dir, "bundles")
	blobStore, err := istore.NewBlobStore(blobDir, debugLog)
	if err != nil {
		return
	}
	fsEval := fseval.DefaultFsEval
	if rootless {
		fsEval = fseval.RootlessFsEval
	}
	mtreeStore, err := istore.NewMtreeStore(mtreeDir, fsEval)
	if err != nil {
		return
	}
	blobStoreExt := istore.NewBlobStoreExt(&blobStore, &mtreeStore, debugLog)
	rostore, err := istore.NewImageStoreRO(imageDir, &blobStoreExt, errorLog)
	if err != nil {
		return
	}
	r = &Store{}
	r.ImageStore, err = istore.NewImageStore(rostore, systemContext)
	if err != nil {
		return
	}
	r.BundleStore, err = bstore.NewBundleStore(bundleDir, debugLog)
	return
}
