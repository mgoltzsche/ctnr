package store

import (
	"io"
	"os"
	"runtime"

	"github.com/mgoltzsche/cntnr/pkg/log"
	"github.com/openSUSE/umoci/oci/layer"
	digest "github.com/opencontainers/go-digest"
	ispecs "github.com/opencontainers/image-spec/specs-go/v1"
	rspecs "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/pkg/errors"
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

// Unpacks all layers contained in the referenced manifest into rootfs
func (s *BlobStoreExt) UnpackLayers(manifestDigest digest.Digest, rootfs string) (err error) {
	defer func() {
		err = errors.Wrap(err, "unpack image layers")
	}()
	manifest, err := s.ImageManifest(manifestDigest)
	if err != nil {
		return
	}
	if err = s.unpackLayers(&manifest, rootfs); err != nil {
		return
	}
	spec, err := s.mtree.Create(rootfs, nil)
	if err != nil {
		return
	}
	if err = s.mtree.Put(manifestDigest, spec); err != nil {
		return
	}
	return
}

// Writes the diff of rootfs to its parent as new layer into the store
func (s *BlobStoreExt) CommitLayer(src *LayerSource, parentManifestDigest *digest.Digest, author, createdBy string) (r *CommitResult, err error) {
	defer func() {
		err = errors.Wrap(err, "commit layer blob")
	}()

	// Load parent
	parentMtree := &mtree.DirectoryHierarchy{}
	var manifest ispecs.Manifest
	r = &CommitResult{}
	if parentManifestDigest != nil {
		manifest, err = s.ImageManifest(*parentManifestDigest)
		if err != nil {
			return
		}
		if r.Config, err = s.ImageConfig(manifest.Config.Digest); err != nil {
			return
		}
		if !src.delta {
			parentMtree, err = s.mtree.Get(*parentManifestDigest)
			if err != nil {
				return
			}
		}
	}

	// Diff file system
	reader, err := s.diff(parentMtree, src.rootfsMtree, src.rootfs)
	if err != nil {
		return
	}
	defer reader.Close()

	// Save layer
	var diffIdDigest digest.Digest
	layer, diffIdDigest, err := s.PutLayer(reader)
	if err != nil {
		return
	}

	// Update config
	if createdBy == "" {
		createdBy = "layer"
	}
	r.Config.History = append(r.Config.History, ispecs.History{
		Author:     author,
		CreatedBy:  createdBy,
		EmptyLayer: false,
	})
	r.Config.RootFS.DiffIDs = append(r.Config.RootFS.DiffIDs, diffIdDigest)
	configDescriptor, err := s.PutImageConfig(r.Config)
	if err != nil {
		return
	}

	// Update manifest
	manifest.Config = configDescriptor
	manifest.Layers = append(manifest.Layers, layer)
	r.Manifest = manifest
	if r.Descriptor, err = s.PutImageManifest(manifest); err != nil {
		return
	}
	r.Descriptor.MediaType = ispecs.MediaTypeImageManifest
	r.Descriptor.Platform = &ispecs.Platform{
		Architecture: runtime.GOARCH,
		OS:           runtime.GOOS,
	}

	// Save mtree for new manifest
	if !src.delta {
		if err = s.mtree.Put(r.Descriptor.Digest, src.rootfsMtree); err != nil {
			return
		}
	}
	return
}

func (s *BlobStoreExt) diff(from, to *mtree.DirectoryHierarchy, rootfs string) (io.ReadCloser, error) {
	// Read parent/last mtree
	diffs, err := s.mtree.Diff(from, to)
	if err != nil {
		return nil, errors.Wrap(err, "diff")
	}

	if len(diffs) == 0 {
		return nil, errors.New("empty diff")
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
		return nil, errors.Wrap(err, "diff")
	}

	return reader, nil
}
