package store

import (
	"fmt"
	"io"
	"os"
	"runtime"
	"time"

	"github.com/mgoltzsche/cntnr/log"
	"github.com/openSUSE/umoci/oci/layer"
	digest "github.com/opencontainers/go-digest"
	ispecs "github.com/opencontainers/image-spec/specs-go/v1"
	rspecs "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/vbatts/go-mtree"
)

type BlobStoreExt struct {
	*BlobStore
	mtree    *MtreeStore
	rootless bool
	debug    log.Logger
}

type CommitResult struct {
	Manifest   ispecs.Manifest
	Config     ispecs.Image
	Descriptor ispecs.Descriptor
}

func NewBlobStoreExt(blobStore *BlobStore, mtreeStore *MtreeStore, rootless bool, debug log.Logger) BlobStoreExt {
	return BlobStoreExt{blobStore, mtreeStore, rootless, debug}
}

func (s *BlobStoreExt) MarkUsedBlob(id digest.Digest) (err error) {
	now := time.Now()
	if err = os.Chtimes(s.blobFile(id), now, now); err != nil {
		err = fmt.Errorf("mark used blob: %s", err)
	}
	return
}

func (s *BlobStoreExt) UnpackLayers(manifestDigest digest.Digest, rootfs string) (err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("unpack image layers: %s", err)
		}
	}()
	if err = s.MarkUsedBlob(manifestDigest); err != nil {
		return
	}
	manifest, err := s.ImageManifest(manifestDigest)
	if err != nil {
		return
	}
	if err = s.unpackLayers(&manifest, rootfs); err != nil {
		return
	}
	spec, err := s.mtree.Create(rootfs)
	if err != nil {
		return
	}
	if err = s.mtree.Put(manifestDigest, spec); err != nil {
		return
	}
	return
}

func (s *BlobStoreExt) CommitLayer(rootfs string, parentManifestDigest *digest.Digest, author, comment string) (r *CommitResult, err error) {
	// Load parent
	var parentMtree *mtree.DirectoryHierarchy
	var manifest ispecs.Manifest
	r = &CommitResult{}
	if parentManifestDigest != nil {
		manifest, err = s.ImageManifest(*parentManifestDigest)
		if err != nil {
			return r, fmt.Errorf("commit: %s", err)
		}
		if r.Config, err = s.ImageConfig(manifest.Config.Digest); err != nil {
			return r, fmt.Errorf("commit: %s", err)
		}
		parentMtree, err = s.mtree.Get(*parentManifestDigest)
		if err != nil {
			return r, fmt.Errorf("commit: %s", err)
		}
	}

	// Diff file system
	containerMtree, err := s.mtree.Create(rootfs)
	if err != nil {
		return r, fmt.Errorf("commit: %s", err)
	}
	reader, err := s.diff(parentMtree, containerMtree, rootfs)
	if err != nil {
		return r, fmt.Errorf("commit: %s", err)
	}
	defer reader.Close()

	// Save layer
	var diffIdDigest digest.Digest
	layer, diffIdDigest, err := s.PutLayer(reader)
	if err != nil {
		return r, fmt.Errorf("commit: %s", err)
	}

	// Update config
	if comment == "" {
		comment = "layer"
	}
	historyEntry := ispecs.History{
		CreatedBy:  author,
		Comment:    comment,
		EmptyLayer: false,
	}
	if r.Config.History == nil {
		r.Config.History = []ispecs.History{historyEntry}
	} else {
		r.Config.History = append(r.Config.History, historyEntry)
	}
	if r.Config.RootFS.DiffIDs == nil {
		r.Config.RootFS.DiffIDs = []digest.Digest{diffIdDigest}
	} else {
		r.Config.RootFS.DiffIDs = append(r.Config.RootFS.DiffIDs, diffIdDigest)
	}
	configDescriptor, err := s.PutImageConfig(r.Config)
	if err != nil {
		return
	}

	// Update manifest
	manifest.Config = configDescriptor
	if manifest.Layers == nil {
		manifest.Layers = []ispecs.Descriptor{layer}
	} else {
		manifest.Layers = append(manifest.Layers, layer)
	}
	r.Manifest = manifest
	if r.Descriptor, err = s.PutImageManifest(manifest); err != nil {
		return r, fmt.Errorf("commit: %s", err)
	}
	r.Descriptor.MediaType = ispecs.MediaTypeImageManifest
	r.Descriptor.Platform = &ispecs.Platform{
		Architecture: runtime.GOARCH,
		OS:           runtime.GOOS,
	}

	// Save mtree for new manifest
	if err = s.mtree.Put(r.Descriptor.Digest, containerMtree); err != nil {
		return r, fmt.Errorf("commit: %s", err)
	}
	return
}

func (s *BlobStoreExt) diff(from, to *mtree.DirectoryHierarchy, rootfs string) (io.ReadCloser, error) {
	// Read parent/last mtree
	diffs, err := s.mtree.Diff(from, to)
	if err != nil {
		return nil, fmt.Errorf("diff: %s", err)
	}

	if len(diffs) == 0 {
		return nil, fmt.Errorf("empty diff")
	}

	// Generate tar layer from mtree diff
	var opts *layer.MapOptions
	if s.rootless {
		opts = &layer.MapOptions{
			UIDMappings: []rspecs.LinuxIDMapping{{HostID: uint32(os.Geteuid()), ContainerID: 0, Size: 1}},
			GIDMappings: []rspecs.LinuxIDMapping{{HostID: uint32(os.Getegid()), ContainerID: 0, Size: 1}},
			Rootless:    s.rootless,
		}
	}
	reader, err := layer.GenerateLayer(rootfs, diffs, opts)
	if err != nil {
		return nil, fmt.Errorf("diff: %s", err)
	}

	return reader, nil
}
