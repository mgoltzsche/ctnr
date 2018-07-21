package store

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"os"
	"path/filepath"

	"github.com/mgoltzsche/cntnr/image"
	"github.com/mgoltzsche/cntnr/pkg/fs"
	"github.com/mgoltzsche/cntnr/pkg/fs/source"
	"github.com/mgoltzsche/cntnr/pkg/fs/tree"
	fswriter "github.com/mgoltzsche/cntnr/pkg/fs/writer"
	"github.com/mgoltzsche/cntnr/pkg/log"
	"github.com/openSUSE/umoci/oci/layer"
	digest "github.com/opencontainers/go-digest"
	ispecs "github.com/opencontainers/image-spec/specs-go/v1"
	rspecs "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/pkg/errors"
	"github.com/vbatts/go-mtree"
)

type BlobStoreOci struct {
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

func NewBlobStoreExt(blobStore *BlobStore, mtreeStore *MtreeStore, rootless bool, debug log.Logger) BlobStoreOci {
	return BlobStoreOci{blobStore, mtreeStore, rootless, debug}
}

func (s *BlobStoreOci) ImageManifest(manifestDigest digest.Digest) (r ispecs.Manifest, err error) {
	b, err := s.readBlob(manifestDigest)
	if err != nil {
		return r, errors.WithMessage(err, "image manifest")
	}
	if err = json.Unmarshal(b, &r); err != nil {
		return r, errors.Errorf("unmarshal image manifest %s: %s", manifestDigest, err)
	}
	if r.Config.Digest.String() == "" {
		return r, errors.Errorf("image manifest: loaded JSON blob %q is not an OCI image manifest", manifestDigest)
	}
	return
}

func (s *BlobStoreOci) putImageManifest(m ispecs.Manifest) (d ispecs.Descriptor, err error) {
	d.Digest, d.Size, err = s.putJsonBlob(m)
	d.MediaType = ispecs.MediaTypeImageManifest
	return d, errors.WithMessage(err, "put image manifest")
}

func (s *BlobStoreOci) ImageConfig(configDigest digest.Digest) (r ispecs.Image, err error) {
	b, err := s.readBlob(configDigest)
	if err != nil {
		return r, errors.WithMessage(err, "image config")
	}
	if err = json.Unmarshal(b, &r); err != nil {
		err = errors.Errorf("unmarshal image config %s: %s", configDigest, err)
	}
	return
}

func (s *BlobStoreOci) getMtree(manifestDigest digest.Digest) (*mtree.DirectoryHierarchy, error) {
	return s.mtree.Get(manifestDigest)
}

func (s *BlobStoreOci) PutImageConfig(cfg ispecs.Image, parentManifest *digest.Digest, m modifier) (d ispecs.Descriptor, manifest ispecs.Manifest, err error) {
	d, manifest, err = s.putImageConfig(cfg, parentManifest, func(*ispecs.Manifest) {})
	if err == nil {
		// Since a config change does not result in different mtree use the parent
		// image's mtree file as this child's mtree.
		// It must be copied HERE to support efficient file system diff when
		// committing an already existing container using this image as parent
		// (happens in ImageBuilder)
		var dh *mtree.DirectoryHierarchy
		if parentManifest != nil {
			pdh, e := s.mtree.Get(*parentManifest)
			if e == nil {
				dh = pdh
				err = s.mtree.Put(d.Digest, dh)
			} else if !IsMtreeNotExist(e) {
				err = e
			}
		}
		err = errors.WithMessage(err, "put image config")
	}
	return
}

type modifier func(m *ispecs.Manifest)

func (s *BlobStoreOci) putImageConfig(cfg ispecs.Image, parentManifest *digest.Digest, m modifier) (d ispecs.Descriptor, manifest ispecs.Manifest, err error) {
	d.MediaType = ispecs.MediaTypeImageConfig
	if d.Digest, d.Size, err = s.putJsonBlob(cfg); err != nil {
		return
	}

	// Create new manifest
	if parentManifest == nil {
		manifest = ispecs.Manifest{}
		manifest.Versioned.SchemaVersion = 2
	} else {
		if manifest, err = s.ImageManifest(*parentManifest); err != nil {
			err = errors.WithMessage(err, "put image config: parent manifest")
			return
		}
		m(&manifest)
	}

	manifest.Config = d
	d, err = s.putImageManifest(manifest)
	d.Platform = &ispecs.Platform{
		Architecture: cfg.Architecture,
		OS:           cfg.OS,
	}
	err = errors.WithMessage(err, "put image config")
	return
}

func (s *BlobStoreOci) putJsonBlob(o interface{}) (d digest.Digest, size int64, err error) {
	var buf bytes.Buffer
	if err = json.NewEncoder(&buf).Encode(o); err != nil {
		return d, size, errors.New("put json blob: " + err.Error())
	}
	return s.putBlob(&buf)
}

// TODO: use this also for image file system extraction
func (s *BlobStoreOci) LayerFS(manifestDigest digest.Digest) (r fs.FsNode, err error) {
	defer func() {
		err = errors.Wrap(err, "load fs from layers")
	}()
	manifest, err := s.ImageManifest(manifestDigest)
	if err != nil {
		return
	}
	r = tree.NewFS()
	for _, l := range manifest.Layers {
		d := l.Digest
		layerFile := filepath.Join(s.blobDir, d.Algorithm().String(), d.Hex())
		if _, err = r.AddUpper("/", source.NewSourceTarGz(layerFile)); err != nil {
			return
		}
	}
	return
}

func (s *BlobStoreOci) NormalizedLayerFS(manifestDigest digest.Digest) (r fs.FsNode, err error) {
	// TODO: cache whole operation (storing FsNode instead of mtree)
	if r, err = s.LayerFS(manifestDigest); err != nil {
		return
	}
	return r.Normalized()
}

func (s *BlobStoreOci) AddLayer(rootfs fs.FsNode, parentManifestDigest *digest.Digest, author, createdBy string) (r *CommitResult, err error) {
	// Load parent
	//parentFs := tree.NewFS()
	var parentFs fs.FsNode
	r = &CommitResult{}
	if parentManifestDigest != nil {
		var parentManifest ispecs.Manifest
		parentManifest, err = s.ImageManifest(*parentManifestDigest)
		if err == nil {
			if r.Config, err = s.ImageConfig(parentManifest.Config.Digest); err == nil && len(parentManifest.Layers) > 0 {
				parentFs, err = s.NormalizedLayerFS(*parentManifestDigest)
			}
		}
		if err != nil {
			return nil, errors.WithMessage(err, "put layer: parent")
		}
		if s.rootless {
			// Convert devices to files since dirwriter does so in rootless mode.
			// (If this wouldn't be done device files contained within a parent
			// image would become regular files on commit)
			parentFs.MockDevices()
		}
	}
	// Create new layer as delta from parent
	layerFs, err := parentFs.Diff(rootfs)
	if err != nil {
		return nil, errors.WithMessage(err, "put layer")
	}

	if layerFs.Empty() {
		return nil, image.ErrorEmptyLayerDiff("empty layer")
	}
	var layerStr bytes.Buffer
	if err = layerFs.WriteTo(&layerStr, fs.AttrsMtime); err != nil {
		return nil, errors.WithMessage(err, "put layer")
	}
	s.debug.Printf("Adding layer:\n  parent manifest: %s\n  contents:\n%s", parentManifestDigest, layerStr.String())

	// Save layer
	tarReader := s.generateTar(layerFs)
	defer tarReader.Close()
	layerDescriptor, diffIdDigest, err := s.BlobStore.PutLayer(tarReader)
	if err != nil {
		return
	}

	// Create new config and manifest
	if createdBy == "" {
		createdBy = "layer"
	}
	r.Config.History = append(r.Config.History, ispecs.History{
		Author:     author,
		CreatedBy:  createdBy,
		EmptyLayer: false,
	})
	r.Config.RootFS.DiffIDs = append(r.Config.RootFS.DiffIDs, diffIdDigest)
	r.Descriptor, r.Manifest, err = s.putImageConfig(r.Config, parentManifestDigest, func(m *ispecs.Manifest) {
		m.Layers = append(m.Layers, layerDescriptor)
	})
	r.Descriptor.MediaType = ispecs.MediaTypeImageManifest
	r.Descriptor.Platform = &ispecs.Platform{
		Architecture: r.Config.Architecture,
		OS:           r.Config.OS,
	}
	return
}

func (s *BlobStoreOci) generateTar(rootfs fs.FsNode) io.ReadCloser {
	reader, writer := io.Pipe()
	go func() (err error) {
		// Close writer with the returned error.
		defer func() {
			writer.CloseWithError(errors.Wrap(err, "generate layer tar"))
		}()
		// Writer tar
		tarWriter := fswriter.NewTarWriter(writer)
		defer func() {
			if e := tarWriter.Close(); e != nil && err == nil {
				err = e
			}
		}()
		return rootfs.Write(tarWriter)
	}()
	return reader
}

// Unpacks all layers contained in the referenced manifest into rootfs
func (s *BlobStoreOci) UnpackLayers(manifestDigest digest.Digest, rootfs string) (err error) {
	defer func() {
		err = errors.Wrap(err, "unpack image layers")
	}()
	manifest, err := s.ImageManifest(manifestDigest)
	if err != nil {
		return
	}
	// ATTENTION: rootfs must be a new empty directory to guarantee that the
	// derived mtree represents the manifestDigest and doesn't get mixed up with
	// other existing files
	if err = os.Mkdir(rootfs, 0755); err != nil {
		return errors.New(err.Error())
	}
	if err = s.unpackLayers(&manifest, rootfs); err != nil {
		return
	}
	exists, err := s.mtree.Exists(manifestDigest)
	if err != nil {
		return
	}
	if !exists {
		var spec *mtree.DirectoryHierarchy
		spec, err = s.mtree.Create(rootfs)
		if err != nil {
			return
		}
		err = s.mtree.Put(manifestDigest, spec)
	}
	return
}

// Writes the diff of rootfs to its parent as new layer into the store
func (s *BlobStoreOci) PutImageLayer(src *LayerSource, parentManifestDigest *digest.Digest, author, createdBy string) (r *CommitResult, err error) {
	parentStr := ""
	if parentManifestDigest != nil {
		parentStr = " using parent " + (*parentManifestDigest).Hex()[:13]
	}
	s.debug.Println("Building layer from " + src.rootfs + parentStr)

	defer func() {
		err = errors.WithMessage(err, "generate layer blob")
	}()

	// Load parent
	parentMtree := &mtree.DirectoryHierarchy{}
	var manifest ispecs.Manifest
	r = &CommitResult{}
	if parentManifestDigest != nil {
		if manifest, err = s.ImageManifest(*parentManifestDigest); err == nil {
			if r.Config, err = s.ImageConfig(manifest.Config.Digest); err == nil {
				if !src.deltafs {
					parentMtree, err = s.getMtree(*parentManifestDigest)
				}
			}
		}
		if err != nil {
			return nil, errors.WithMessage(err, "parent")
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

	// Create new config and manifest
	if createdBy == "" {
		createdBy = "layer"
	}
	r.Config.History = append(r.Config.History, ispecs.History{
		Author:     author,
		CreatedBy:  createdBy,
		EmptyLayer: false,
	})
	r.Config.RootFS.DiffIDs = append(r.Config.RootFS.DiffIDs, diffIdDigest)
	r.Descriptor, r.Manifest, err = s.putImageConfig(r.Config, parentManifestDigest, func(m *ispecs.Manifest) {
		m.Layers = append(m.Layers, layer)
	})
	r.Descriptor.MediaType = ispecs.MediaTypeImageManifest
	r.Descriptor.Platform = &ispecs.Platform{
		Architecture: r.Config.Architecture,
		OS:           r.Config.OS,
	}

	// Save mtree for new manifest
	if err == nil && !src.deltafs {
		err = s.mtree.Put(r.Descriptor.Digest, src.rootfsMtree)
	}
	return
}

func (s *BlobStoreOci) diff(from, to *mtree.DirectoryHierarchy, rootfs string) (io.ReadCloser, error) {
	// Read parent/last mtree
	diffs, err := s.mtree.Diff(from, to)
	if err != nil {
		return nil, errors.Wrap(err, "diff")
	}

	if len(diffs) == 0 {
		return nil, image.ErrorEmptyLayerDiff("empty layer")
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

func (s *BlobStoreOci) unpackLayers(manifest *ispecs.Manifest, dest string) (err error) {
	defer func() {
		err = errors.Wrap(err, "unpack layers")
	}()

	// Create destination directory
	if _, err = os.Stat(dest); err != nil {
		return errors.New(err.Error())
	}

	// Unpack layers
	for _, l := range manifest.Layers {
		if err = s.unpackLayer(l.Digest, dest); err != nil {
			return
		}
	}
	return
}

func (s *BlobStoreOci) unpackLayer(id digest.Digest, dest string) (err error) {
	s.debug.Println("Extracting layer", id)
	layerFile := filepath.Join(s.blobDir, id.Algorithm().String(), id.Hex())
	f, err := os.Open(layerFile)
	if err != nil {
		return errors.New(err.Error())
	}
	defer f.Close()
	reader, err := gzip.NewReader(f)
	if err != nil {
		return errors.New(err.Error())
	}
	// TODO: add uid/gid mappings
	return layer.UnpackLayer(dest, reader, &layer.MapOptions{Rootless: true})
}
