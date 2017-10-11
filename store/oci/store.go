package oci

import (
	"fmt"
	"path/filepath"

	"github.com/containers/image/types"
	"github.com/mgoltzsche/cntnr/log"
	"github.com/openSUSE/umoci/pkg/fseval"
)

type Store struct {
	*ImageStoreImpl
	*BundleStore
}

func OpenStore(dir string, rootless bool, systemContext *types.SystemContext, errorLog log.Logger, debugLog log.Logger) (r Store, err error) {
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
	blobStore, err := NewBlobStore(blobDir, debugLog)
	if err != nil {
		return
	}
	fsEval := fseval.DefaultFsEval
	if rootless {
		fsEval = fseval.RootlessFsEval
	}
	mtreeStore, err := NewMtreeStore(mtreeDir, fsEval)
	if err != nil {
		return
	}
	blobStoreExt := NewBlobStoreExt(&blobStore, &mtreeStore, debugLog)
	r.ImageStoreImpl, err = NewImageStore(imageDir, &blobStoreExt, systemContext, errorLog)
	if err != nil {
		return
	}
	r.BundleStore, err = NewBundleStore(bundleDir, debugLog)
	return
}
