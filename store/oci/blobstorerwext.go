package oci

import (
	"fmt"
	"io"
	"os"
	"runtime"

	"github.com/mgoltzsche/cntnr/log"
	"github.com/mgoltzsche/cntnr/store"
	"github.com/openSUSE/umoci/oci/layer"
	digest "github.com/opencontainers/go-digest"
	ispecs "github.com/opencontainers/image-spec/specs-go/v1"
	rspecs "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/vbatts/go-mtree"
)

type BlobStoreExt struct {
	*BlobStore
	mtree *MtreeStore
	debug log.Logger
}

func NewBlobStoreExt(blobStore *BlobStore, mtreeStore *MtreeStore, debug log.Logger) BlobStoreExt {
	return BlobStoreExt{blobStore, mtreeStore, debug}
}

func (s *BlobStoreExt) UnpackLayers(manifestDigest digest.Digest, rootfs string) (err error) {
	manifest, err := s.ImageManifest(manifestDigest)
	if err != nil {
		return fmt.Errorf("unpack image: %s", err)
	}
	if err = s.unpackLayers(&manifest, rootfs); err != nil {
		return fmt.Errorf("unpack image: %s", err)
	}
	spec, err := s.mtree.Create(rootfs)
	if err != nil {
		return fmt.Errorf("unpack image: %s", err)
	}
	if err = s.mtree.Put(manifestDigest, spec); err != nil {
		return fmt.Errorf("unpack image: %s", err)
	}
	return
}

func (s *BlobStoreExt) CommitLayer(rootfs string, parentManifestDigest *digest.Digest, author, comment string) (r store.CommitResult, err error) {
	// Load parent
	var parentMtree *mtree.DirectoryHierarchy
	var manifest ispecs.Manifest
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
	reader, err := layer.GenerateLayer(rootfs, diffs, &layer.MapOptions{
		UIDMappings: []rspecs.LinuxIDMapping{{HostID: uint32(os.Geteuid()), ContainerID: 0, Size: 1}},
		GIDMappings: []rspecs.LinuxIDMapping{{HostID: uint32(os.Getegid()), ContainerID: 0, Size: 1}},
		Rootless:    true,
	})
	if err != nil {
		return nil, fmt.Errorf("diff: %s", err)
	}

	return reader, nil
}
