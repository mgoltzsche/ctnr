package store

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"os"
	"runtime"
	"time"

	"github.com/mgoltzsche/ctnr/image"
	"github.com/mgoltzsche/ctnr/pkg/fs"
	"github.com/mgoltzsche/ctnr/pkg/fs/source"
	"github.com/mgoltzsche/ctnr/pkg/fs/tree"
	"github.com/mgoltzsche/ctnr/pkg/fs/writer"
	fswriter "github.com/mgoltzsche/ctnr/pkg/fs/writer"
	"github.com/mgoltzsche/ctnr/pkg/log"
	digest "github.com/opencontainers/go-digest"
	ispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
)

type OCIBlobStore struct {
	*ContentAddressableStore
	fsspecs  *FsSpecStore
	rootless bool
	warn     log.Logger
	debug    log.Logger
}

type CommitResult struct {
	Manifest   ispecs.Manifest
	Config     ispecs.Image
	Descriptor ispecs.Descriptor
}

func NewOCIBlobStore(blobStore *ContentAddressableStore, fsSpecStore *FsSpecStore, rootless bool, warn log.Logger, debug log.Logger) OCIBlobStore {
	return OCIBlobStore{blobStore, fsSpecStore, rootless, warn, debug}
}

func (s *OCIBlobStore) ImageManifest(manifestDigest digest.Digest) (r ispecs.Manifest, err error) {
	reader, err := s.Get(manifestDigest)
	if err != nil {
		return r, errors.WithMessage(err, "image manifest")
	}
	defer reader.Close()
	if err = json.NewDecoder(reader).Decode(&r); err != nil {
		return r, errors.Wrapf(err, "unmarshal image manifest %s", manifestDigest)
	}
	if r.Config.Digest.String() == "" {
		return r, errors.Errorf("image manifest: loaded JSON blob %q is not an OCI image manifest", manifestDigest)
	}
	return
}

func (s *OCIBlobStore) putImageManifest(m ispecs.Manifest) (d ispecs.Descriptor, err error) {
	d.Digest, d.Size, err = s.putJsonBlob(m)
	d.MediaType = ispecs.MediaTypeImageManifest
	return d, errors.WithMessage(err, "put image manifest")
}

func (s *OCIBlobStore) ImageConfig(configDigest digest.Digest) (r ispecs.Image, err error) {
	reader, err := s.Get(configDigest)
	if err != nil {
		return r, errors.WithMessage(err, "image config")
	}
	if err = json.NewDecoder(reader).Decode(&r); err != nil {
		err = errors.Errorf("unmarshal image config %s: %s", configDigest, err)
	}
	return
}

func (s *OCIBlobStore) PutImageConfig(cfg ispecs.Image, parentManifestId *digest.Digest) (d ispecs.Descriptor, manifest ispecs.Manifest, err error) {
	manifest.Versioned.SchemaVersion = 2
	if parentManifestId != nil {
		if manifest, err = s.ImageManifest(*parentManifestId); err != nil {
			err = errors.WithMessage(err, "put image config: parent manifest")
			return
		}
		// Add parent manifest annotation to respect the dependency during garbage collection
		if manifest.Annotations == nil {
			manifest.Annotations = map[string]string{}
		}
		manifest.Annotations[AnnotationParentManifest] = parentManifestId.String()
	}
	d, err = s.putImageConfig(cfg, &manifest)
	return
}

func (s *OCIBlobStore) putImageConfig(cfg ispecs.Image, manifest *ispecs.Manifest) (d ispecs.Descriptor, err error) {
	d.MediaType = ispecs.MediaTypeImageConfig
	if d.Digest, d.Size, err = s.putJsonBlob(cfg); err != nil {
		return
	}
	manifest.Config = d
	d, err = s.putImageManifest(*manifest)
	d.Platform = &ispecs.Platform{
		Architecture: cfg.Architecture,
		OS:           cfg.OS,
	}
	err = errors.WithMessage(err, "put image config")
	return
}

func (s *OCIBlobStore) putJsonBlob(o interface{}) (d digest.Digest, size int64, err error) {
	var buf bytes.Buffer
	if err = json.NewEncoder(&buf).Encode(o); err != nil {
		return d, size, errors.New("put json blob: " + err.Error())
	}
	return s.Put(&buf)
}

func layerChainID(cfg *ispecs.Image) (r digest.Digest, err error) {
	if len(cfg.RootFS.DiffIDs) == 0 {
		return r, errors.New("No layer diffIDs contained in image config")
	}
	if cfg.RootFS.Type != "layers" {
		return r, errors.Errorf("layerChainID: unsupported rootfs type %q found in image config", cfg.RootFS.Type)
	}
	return chainID(cfg.RootFS.DiffIDs), nil
}

// Generates the Layer ChainID as digest of all layers.
// See https://github.com/opencontainers/image-spec/blob/master/config.md#layer-chainid
func chainID(layerIds []digest.Digest) (r digest.Digest) {
	n := len(layerIds)
	switch {
	case n > 1:
		r = digest.FromString(chainID(layerIds[:n-1]).String() + " " + layerIds[n-1].String())
	case n == 1:
		r = layerIds[n-1]
	}
	return
}

func (s *OCIBlobStore) FS(manifestDigest digest.Digest) (r fs.FsNode, err error) {
	defer func() {
		err = errors.Wrap(err, "load fs from layers")
	}()
	manifest, err := s.ImageManifest(manifestDigest)
	if err != nil {
		return
	}
	return s.fsFromManifest(&manifest)
}

func (s *OCIBlobStore) fsFromManifest(manifest *ispecs.Manifest) (r fs.FsNode, err error) {
	r = tree.NewFS()
	for _, l := range manifest.Layers {
		layerFile, e := s.keyFile(l.Digest)
		if e != nil {
			return nil, errors.Wrap(e, "fsspec from manifest")
		}
		var src fs.Source
		switch l.MediaType {
		case ispecs.MediaTypeImageLayerGzip:
			src = source.NewSourceTarGz(layerFile)
		case ispecs.MediaTypeImageLayer:
			src = source.NewSourceTar(layerFile)
		default:
			return nil, errors.Errorf("unsupported layer media type %q", l.MediaType)
		}
		if _, err = r.AddUpper(".", src); err != nil {
			return
		}
	}
	return
}

func (s *OCIBlobStore) FSSpec(manifestDigest digest.Digest) (r fs.FsNode, err error) {
	manifest, err := s.ImageManifest(manifestDigest)
	if err != nil {
		return
	}
	cfg, err := s.ImageConfig(manifest.Config.Digest)
	if err != nil {
		return
	}
	chainId, err := layerChainID(&cfg)
	if err != nil {
		return
	}
	// Use cached fsspec ...
	if r, err = s.fsspecs.Get(chainId); image.IsNotExist(err) {
		// ... or derive and store new fsspec if cache key not found
		if r, err = s.fsFromManifest(&manifest); err != nil {
			return
		}
		if r, err = r.Normalized(); err != nil {
			return
		}
		err = s.fsspecs.Put(chainId, r)
	}
	return
}

// Creates a new image with a layer containing the provided file system's difference to the parent provided image.
func (s *OCIBlobStore) AddLayer(rootfs fs.FsNode, parentManifestDigest *digest.Digest, author, createdBy string) (r *CommitResult, err error) {
	// Load parent
	parentFs := tree.NewFS()
	r = &CommitResult{}
	now := time.Now()
	r.Config.Created = &now
	r.Config.Architecture = runtime.GOARCH
	r.Config.OS = runtime.GOOS
	r.Config.RootFS.Type = "layers"
	r.Manifest.Versioned.SchemaVersion = 2
	if parentManifestDigest != nil {
		if r.Manifest, err = s.ImageManifest(*parentManifestDigest); err == nil {
			if r.Config, err = s.ImageConfig(r.Manifest.Config.Digest); err == nil && len(r.Manifest.Layers) > 0 {
				parentFs, err = s.FSSpec(*parentManifestDigest)
			}
		}
		if err != nil {
			return nil, errors.WithMessage(err, "put layer: parent")
		}
		// Add parent manifest annotation to respect the dependency during garbage collection
		if r.Manifest.Annotations == nil {
			r.Manifest.Annotations = map[string]string{}
		}
		r.Manifest.Annotations[AnnotationParentManifest] = parentManifestDigest.String()
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
		return nil, image.ErrEmptyLayerDiff(errors.New("empty layer"))
	}
	var layerStr bytes.Buffer
	if err = layerFs.WriteTo(&layerStr, fs.AttrsMtime); err != nil {
		return nil, errors.WithMessage(err, "put layer")
	}
	s.debug.Printf("Adding layer:\n  parent manifest: %s\n  contents:\n%s", parentManifestDigest, layerStr.String())

	// Save layer
	tarReader := s.generateTar(layerFs)
	defer func() {
		if e := tarReader.Close(); e != nil && err == nil {
			err = e
		}
	}()
	layerDescriptor, diffIdDigest, err := s.putGz(tarReader)
	if err != nil {
		return
	}

	// Create new config and manifest
	if createdBy == "" {
		createdBy = "layer"
	}
	r.Manifest.Layers = append(r.Manifest.Layers, layerDescriptor)
	r.Config.History = append(r.Config.History, ispecs.History{
		Author:     author,
		CreatedBy:  createdBy,
		EmptyLayer: false,
	})
	r.Config.RootFS.DiffIDs = append(r.Config.RootFS.DiffIDs, diffIdDigest)
	r.Descriptor, err = s.putImageConfig(r.Config, &r.Manifest)
	r.Descriptor.MediaType = ispecs.MediaTypeImageManifest
	r.Descriptor.Platform = &ispecs.Platform{
		Architecture: r.Config.Architecture,
		OS:           r.Config.OS,
	}

	// Cache fsspec
	chainId, err := layerChainID(&r.Config)
	if err != nil {
		return
	}
	rootfs, err = rootfs.Normalized()
	if err != nil {
		return
	}
	err = s.fsspecs.Put(chainId, rootfs)
	return
}

func (s *OCIBlobStore) putGz(reader io.Reader) (layer ispecs.Descriptor, diffIdDigest digest.Digest, err error) {
	// diffID digest
	diffIdDigester := digest.SHA256.Digester()
	hashReader := io.TeeReader(reader, diffIdDigester.Hash())
	pipeReader, pipeWriter := io.Pipe()
	defer pipeReader.Close()

	// gzip
	gzw := gzip.NewWriter(pipeWriter)
	defer gzw.Close()
	go func() {
		if _, err := io.Copy(gzw, hashReader); err != nil {
			pipeWriter.CloseWithError(errors.Wrap(err, "compressing layer blob"))
			return
		}
		gzw.Close()
		pipeWriter.Close()
	}()

	// Write blob
	layer.Digest, layer.Size, err = s.Put(pipeReader)
	if err != nil {
		return
	}
	diffIdDigest = diffIdDigester.Digest()
	layer.MediaType = ispecs.MediaTypeImageLayerGzip
	return
}

func (s *OCIBlobStore) generateTar(rootfs fs.FsNode) io.ReadCloser {
	reader, writer := io.Pipe()
	go func() (err error) {
		// Close writer with the returned error.
		defer func() {
			writer.CloseWithError(errors.Wrap(err, "generate layer tar"))
		}()
		// Write tar
		tarWriter := fswriter.NewTarWriter(writer)
		defer func() {
			if err == nil {
				if e := tarWriter.Close(); e != nil {
					err = e
				}
			}
		}()
		return rootfs.Write(tarWriter)
	}()
	return reader
}

// Unpacks all layers contained in the referenced manifest into rootfs
func (s *OCIBlobStore) UnpackLayers(manifestDigest digest.Digest, dest string) (err error) {
	defer func() {
		err = errors.Wrap(err, "unpack image layers")
	}()
	s.debug.Println("Unpacking layers")
	// TODO: avoid loading manifest + config again (already loaded to build bundle config)
	manifest, err := s.ImageManifest(manifestDigest)
	if err != nil {
		return
	}
	cfg, err := s.ImageConfig(manifest.Config.Digest)
	if err != nil {
		return
	}
	chainId, err := layerChainID(&cfg)
	if err != nil {
		return
	}
	layerfs, err := s.fsFromManifest(&manifest)
	if err != nil {
		return
	}
	// ATTENTION: rootfs must be a new empty directory to guarantee that the
	// derived mtree represents the manifestDigest and doesn't get mixed up with
	// other existing files
	if err = os.Mkdir(dest, 0775); err != nil {
		return
	}
	dirWriter := writer.NewDirWriter(dest, fs.NewFSOptions(s.rootless), s.warn)
	var fsWriter fs.Writer = dirWriter
	fsspecExists, err := s.fsspecs.Exists(chainId)
	if err != nil {
		return
	}
	if !fsspecExists {
		// Generate fsspec on-the-fly during unpacking if not exists
		fsspec := tree.NewFS()
		fsWriter = writer.NewFsNodeWriter(fsspec, fsWriter)
		fsWriter = writer.NewHashingWriter(fsWriter)
		defer func() {
			if err == nil {
				if err = s.fsspecs.Put(chainId, fsspec); err != nil {
					return
				}
			}
		}()
	}
	if err = layerfs.Write(fsWriter); err != nil {
		return
	}
	return dirWriter.Close()
}
