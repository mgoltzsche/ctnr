package store

import (
	"path/filepath"

	"github.com/containers/image/types"
	"github.com/mgoltzsche/ctnr/bundle"
	bstore "github.com/mgoltzsche/ctnr/bundle/store"
	"github.com/mgoltzsche/ctnr/image"
	istore "github.com/mgoltzsche/ctnr/image/store"
	"github.com/mgoltzsche/ctnr/pkg/lock"
	"github.com/mgoltzsche/ctnr/pkg/log"
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

func NewStore(dir string, rootless bool, systemContext *types.SystemContext, trustPolicy istore.TrustPolicyContext, loggers log.Loggers) (r Store, err error) {
	if dir == "" {
		return r, errors.New("init store: no store directory provided")
	}
	dir, err = filepath.Abs(dir)
	if err != nil {
		return r, errors.Wrap(err, "init store")
	}
	locker, err := lock.NewExclusiveDirLocker(dir)
	if err != nil {
		return r, errors.Wrap(err, "init store")
	}
	blobDir := filepath.Join(dir, "blobs")
	fsspecDir := filepath.Join(dir, ".fsspec")
	imageRepoDir := filepath.Join(dir, "image-repos")
	imageIdDir := filepath.Join(dir, "image-ids")
	bundleDir := filepath.Join(dir, "bundles")
	tempDir := filepath.Join(dir, ".temp")
	mtreeStore := istore.NewFsSpecStore(fsspecDir, loggers.Debug)
	blobStore := istore.NewContentAddressableStore(blobDir)
	blobStoreExt := istore.NewOCIBlobStore(&blobStore, &mtreeStore, rootless, loggers.Warn, loggers.Debug)
	rostore := istore.NewImageStoreRO(imageRepoDir, &blobStoreExt, istore.NewImageIdStore(imageIdDir), loggers.Warn)
	r.ImageStore = istore.NewImageStore(locker, rostore, tempDir, systemContext, trustPolicy, rootless, loggers)
	r.BundleStore = bstore.NewBundleStore(bundleDir, loggers.Info, loggers.Debug)
	return
}
