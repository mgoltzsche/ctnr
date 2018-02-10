package store

import (
	"path/filepath"

	"github.com/containers/image/types"
	"github.com/mgoltzsche/cntnr/log"
	"github.com/mgoltzsche/cntnr/oci/bundle"
	bstore "github.com/mgoltzsche/cntnr/oci/bundle/store"
	"github.com/mgoltzsche/cntnr/oci/image"
	istore "github.com/mgoltzsche/cntnr/oci/image/store"
	"github.com/openSUSE/umoci/pkg/fseval"
	"github.com/pkg/errors"
)

// Minimal Store interface.
// containers/storage interface is not used to ease the OCI store implementation
// which is required by unprivileged users (https://github.com/containers/storage/issues/96)

var _ image.ImageStore = &Store{}

type Store struct {
	image.ImageStore
	bundle.BundleStore
}

type LockedStore struct {
	image.ImageStoreRW
	bundle.BundleStore
}

/*func (s *Store) ImageBuilderFromBundle(bundle bundle.Bundle, author string) (b *builder.ImageBuilder, err error) {
	rwstore, err := s.OpenLockedImageStore()
	if err != nil {
		return
	}
	if b, err = builder.NewImageBuilderFromBundle(rwstore, bundle, author); err != nil {
		err = errors.Wrap(err, "image builder from bundle")
		rwstore.Close()
	}
	return
}*/

func NewStore(dir string, rootless bool, systemContext *types.SystemContext, trustPolicy istore.TrustPolicyContext, loggers log.Loggers) (r Store, err error) {
	if dir == "" {
		return r, errors.New("init store: no store directory provided")
	}
	dir, err = filepath.Abs(dir)
	if err == nil {
		blobDir := filepath.Join(dir, "blobs")
		mtreeDir := filepath.Join(dir, "mtree")
		imageRepoDir := filepath.Join(dir, "image-repos")
		imageIdDir := filepath.Join(dir, "image-ids")
		bundleDir := filepath.Join(dir, "bundles")
		fsEval := fseval.DefaultFsEval
		if rootless {
			fsEval = fseval.RootlessFsEval
		}
		mtreeStore := istore.NewMtreeStore(mtreeDir, fsEval)
		blobStore := istore.NewBlobStore(blobDir, loggers.Debug)
		blobStoreExt := istore.NewBlobStoreExt(&blobStore, &mtreeStore, rootless, loggers.Debug)
		rostore := istore.NewImageStoreRO(imageRepoDir, &blobStoreExt, istore.NewImageIdStore(imageIdDir), loggers.Error)
		r.ImageStore, err = istore.NewImageStore(rostore, systemContext, trustPolicy, loggers.Error)
		if err == nil {
			r.BundleStore, err = bstore.NewBundleStore(bundleDir, loggers.Debug)
		}
	}
	return r, errors.Wrap(err, "init store")
}
