package oci

import (
	"fmt"

	"os"
	"path/filepath"

	"github.com/containers/image/types"
	"github.com/mgoltzsche/cntnr/log"
	"github.com/mgoltzsche/cntnr/store"
	"github.com/openSUSE/umoci/pkg/fseval"
)

type Store struct {
	*ContainerStore
}

var _ store.Store = &Store{}

func NewOCIStore(dir string, rootless bool, systemContext *types.SystemContext, errorLog log.Logger, debugLog log.Logger) (s Store, err error) {
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
	containerDir := filepath.Join(dir, "containers")
	if err = os.MkdirAll(containerDir, 0755); err != nil {
		return
	}
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
	imageStore, err := NewImageStore(imageDir, &blobStoreExt, systemContext, errorLog)
	if err != nil {
		return
	}
	cs, err := NewContainerStore(containerDir, &imageStore, debugLog)
	if err != nil {
		return
	}
	s.ContainerStore = &cs
	return
}
