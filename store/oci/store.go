package oci

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"

	"github.com/containers/image/types"
	"github.com/mgoltzsche/cntnr/generate"
	"github.com/mgoltzsche/cntnr/log"
	"github.com/mgoltzsche/cntnr/store"
	"github.com/openSUSE/umoci/oci/layer"
	"github.com/openSUSE/umoci/pkg/fseval"
	digest "github.com/opencontainers/go-digest"
	ispecs "github.com/opencontainers/image-spec/specs-go/v1"
	rspecs "github.com/opencontainers/runtime-spec/specs-go"
	gen "github.com/opencontainers/runtime-tools/generate"
	"github.com/vbatts/go-mtree"
)

const (
	ANNOTATION_IMAGE_MANIFEST_DIGEST = "com.github.mgoltzsche.cntnr.bundle.image.manifest.digest"
)

type Store struct {
	*ImageStore
	mtree        MtreeStore
	containerDir string
	imageDir     string
	rootless     bool
	err          log.Logger
	debug        log.Logger
}

var _ store.Store = &Store{}

func NewOCIStore(dir string, rootless bool, systemContext *types.SystemContext, errorLog log.Logger, debugLog log.Logger) (s *Store, err error) {
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
	imageStore, err := NewImageStore(imageDir, &blobStore, systemContext, errorLog)
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
	return &Store{
		ImageStore:   &imageStore,
		mtree:        mtreeStore,
		containerDir: containerDir,
		imageDir:     imageDir,
		rootless:     rootless,
		err:          errorLog,
		debug:        debugLog,
	}, nil
}

func (s *Store) Containers() ([]store.Container, error) {
	fl, err := ioutil.ReadDir(s.containerDir)
	if err != nil {
		return nil, fmt.Errorf("containers: %s", err)
	}
	l := make([]store.Container, 0, len(fl))
	for _, f := range fl {
		if f.IsDir() {
			c, err := s.Container(f.Name())
			if err == nil {
				l = append(l, c)
			} else {
				s.debug.Printf("list containers: %s", err)
			}
		}
	}
	return l, nil
}

// TODO: lock container
func (s *Store) Container(id string) (c store.Container, err error) {
	bundleDir := filepath.Join(s.containerDir, id)
	f, err := os.Stat(bundleDir)
	if err != nil {
		err = fmt.Errorf("container not found: %s", err)
	}
	img, err := s.readParent(id)
	if err != nil {
		err = fmt.Errorf("container: %s", err)
	}
	return store.NewContainer(id, bundleDir, img, f.ModTime()), err
}

func (s *Store) DeleteContainer(id string) (err error) {
	if err = os.RemoveAll(filepath.Join(s.containerDir, id)); err != nil {
		err = fmt.Errorf("delete container: %s", err)
	}
	return
}

func (s *Store) CreateContainer(id string, imageManifestDigest *digest.Digest) (b *store.ContainerBuilder, err error) {
	if id == "" {
		id = store.GenerateId()
	}
	spec := generate.NewSpecBuilder()
	spec.SetRootPath("rootfs")
	if imageManifestDigest != nil {
		manifest, err := s.ImageManifest(*imageManifestDigest)
		if err != nil {
			return b, fmt.Errorf("new container: %s", err)
		}
		conf, err := s.ImageConfig(manifest.Config.Digest)
		if err != nil {
			return b, fmt.Errorf("new container: %s", err)
		}
		spec.ApplyImage(conf)
	}
	dir := filepath.Join(s.containerDir, id)
	return store.NewContainerBuilder(id, dir, imageManifestDigest, &spec, s.buildContainer), nil
}

func (s *Store) buildContainer(b *store.ContainerBuilder) (c store.Container, err error) {
	c.Dir = b.Dir
	rootfs := filepath.Join(c.Dir, b.Spec().Root.Path)

	// Create bundle directory
	if err = os.Mkdir(c.Dir, 0770); err != nil {
		err = fmt.Errorf("build container: %s", err)
		return
	}
	defer func() {
		if err != nil {
			os.RemoveAll(c.Dir)
		}
	}()

	// Prepare rootfs
	if b.ImageManifestDigest != nil {
		if err = s.UnpackImage(*b.ImageManifestDigest, rootfs); err != nil {
			return
		}
		if err = s.writeParent(b.ID, *b.ImageManifestDigest); err != nil {
			return c, fmt.Errorf("build container: %s", err)
		}
	} else if err = os.Mkdir(rootfs, 0755); err != nil {
		return c, fmt.Errorf("build container: %s", err)
	}

	// Write runtime config
	confFile := filepath.Join(c.Dir, "config.json")
	err = b.SaveToFile(confFile, gen.ExportOptions{Seccomp: false})
	if err != nil {
		err = fmt.Errorf("write container config.json: %s", err)
	}
	return
}

func (s *Store) UnpackImage(manifestDigest digest.Digest, rootfs string) (err error) {
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

func (s *Store) writeParent(id string, manifest digest.Digest) error {
	parentFile := filepath.Join(s.containerDir, id, "parent")
	return writeFile(parentFile, []byte(manifest.String()))
}

func (s *Store) readParent(id string) (manifest *digest.Digest, err error) {
	parentFile := filepath.Join(s.containerDir, id, "parent")
	if _, e := os.Stat(filepath.Dir(parentFile)); os.IsNotExist(e) {
		return nil, fmt.Errorf("container %q does not exist", id)
	}
	f, err := os.Open(parentFile)
	if err != nil {
		return nil, fmt.Errorf("container manifest digest: %s", err)
	}
	defer f.Close()
	b, err := ioutil.ReadAll(f)
	if err != nil {
		return nil, fmt.Errorf("container manifest digest: %s", err)
	}
	str := string(b)
	if str == "" {
		return nil, nil
	}
	d, err := digest.Parse(str)
	if err != nil {
		err = fmt.Errorf("container manifest digest: %s", err)
	}
	manifest = &d
	return

}

func (s *Store) Commit(containerId, author, comment string) (r store.CommitResult, err error) {
	// Load parent manifest
	manifestDigest, err := s.readParent(containerId)
	if err != nil {
		return r, fmt.Errorf("commit: %s", err)
	}
	var parentMtree *mtree.DirectoryHierarchy
	var manifest ispecs.Manifest
	if manifestDigest != nil {
		manifest, err = s.ImageManifest(*manifestDigest)
		if err != nil {
			return r, fmt.Errorf("commit: %s", err)
		}
		if r.Config, err = s.ImageConfig(manifest.Config.Digest); err != nil {
			return r, fmt.Errorf("commit: %s", err)
		}
		parentMtree, err = s.mtree.Get(*manifestDigest)
		if err != nil {
			return r, fmt.Errorf("commit: %s", err)
		}
	}

	// Diff file system
	rootfs := filepath.Join(s.containerDir, containerId, "rootfs")
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

	// Update container parent
	if err = s.writeParent(containerId, r.Descriptor.Digest); err != nil {
		return r, fmt.Errorf("commit: %s", err)
	}
	return
}

// Generates a diff tar layer from the container's mtree spec.
func (s *Store) diff(from, to *mtree.DirectoryHierarchy, rootfs string) (io.ReadCloser, error) {
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

func findManifestDigest(idx *ispecs.Index, ref string) (d ispecs.Descriptor, err error) {
	refFound := false
	for _, descriptor := range idx.Manifests {
		if descriptor.Annotations[ispecs.AnnotationRefName] == ref {
			refFound = true
			if descriptor.Platform.Architecture == runtime.GOARCH && descriptor.Platform.OS == runtime.GOOS {
				if descriptor.MediaType != ispecs.MediaTypeImageManifest {
					err = fmt.Errorf("unsupported manifest media type %q", descriptor.MediaType)
				}
				return descriptor, err
			}
		}
	}
	if refFound {
		err = fmt.Errorf("no image manifest for architecture %s and OS %s found in image index!", runtime.GOARCH, runtime.GOOS)
	} else {
		err = fmt.Errorf("no image manifest for ref %q found in image index!", ref)
	}
	return
}
