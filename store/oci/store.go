package oci

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"time"

	//"github.com/containers/storage/pkg/idtools"
	"github.com/containers/image/copy"
	ocitransport "github.com/containers/image/oci/layout"
	"github.com/containers/image/signature"
	"github.com/containers/image/transports/alltransports"
	"github.com/containers/image/types"
	"github.com/mgoltzsche/cntnr/generate"
	"github.com/mgoltzsche/cntnr/log"
	"github.com/mgoltzsche/cntnr/store"
	"github.com/openSUSE/umoci/oci/layer"
	digest "github.com/opencontainers/go-digest"
	gen "github.com/opencontainers/runtime-tools/generate"

	ispecs "github.com/opencontainers/image-spec/specs-go/v1"
	rspecs "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/vbatts/go-mtree"
)

const (
	ANNOTATION_IMAGE_MANIFEST_DIGEST = "com.github.mgoltzsche.cntnr.bundle.image.manifest.digest"
)

type Store struct {
	containerDir  string
	imageDir      string
	blobDir       string
	mtreeDir      string
	rootless      bool
	systemContext *types.SystemContext
	trustPolicy   *signature.PolicyContext
	err           log.Logger
	debug         log.Logger
}

var _ store.Store = &Store{}

func NewOCIStore(dir string, rootless bool, systemContext *types.SystemContext, errorLog log.Logger, debugLog log.Logger) (s *Store, err error) {
	dir, err = filepath.Abs(dir)
	if err != nil {
		return
	}
	imageDir := filepath.Join(dir, "images")
	blobDir := filepath.Join(dir, "blobs")
	containerDir := filepath.Join(dir, "containers")
	mtreeDir := filepath.Join(dir, "mtree")
	for _, sdir := range []string{blobDir, imageDir, containerDir, mtreeDir} {
		if err = os.MkdirAll(sdir, 0755); err != nil {
			return
		}
	}
	trustPolicy, err := createTrustPolicyContext()
	if err != nil {
		return
	}
	return &Store{
		containerDir:  containerDir,
		imageDir:      imageDir,
		blobDir:       blobDir,
		mtreeDir:      mtreeDir,
		rootless:      rootless,
		systemContext: systemContext,
		trustPolicy:   trustPolicy,
		err:           errorLog,
		debug:         debugLog,
	}, nil
}

func (s *Store) Close() error {
	return nil
}

func (s *Store) ImportImage(src string) (img store.Image, err error) {
	srcRef, err := alltransports.ParseImageName(src)
	if err != nil {
		err = fmt.Errorf("import: invalid source %q: %s", src, err)
		return
	}

	// Create image directory under new image UUID
	name, ref := store.NameAndRef(srcRef)
	imgDir, err := s.createImageDir(name)
	if err != nil {
		err = fmt.Errorf("import: %s", err)
		return
	}
	defer func() {
		if err != nil {
			os.RemoveAll(imgDir)
		}
	}()

	// Copy image
	destRef, err := ocitransport.Transport.ParseReference(imgDir + ":" + ref)
	if err != nil {
		return img, fmt.Errorf("import: invalid destination %q: %s", imgDir, err)
	}
	err = copy.Image(s.trustPolicy, destRef, srcRef, &copy.Options{
		RemoveSignatures: false,
		SignBy:           "",
		ReportWriter:     os.Stdout,
		SourceCtx:        s.systemContext,
		DestinationCtx:   &types.SystemContext{},
	})
	if err != nil {
		return img, fmt.Errorf("import: %s", err)
	}

	return s.ImageByName(src)
}

func (s *Store) ImageByName(name string) (r store.Image, err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("image %q not found in local store: %s", name, err)
		}
	}()
	if id, e := digest.Parse(name); e == nil && id.Validate() == nil {
		return s.Image(id)
	}
	ref := "latest"
	if imgRef, err := alltransports.ParseImageName(name); err == nil {
		name, ref = store.NameAndRef(imgRef)
	}
	dir := s.imageFile(name)
	idx, err := s.imageIndex(dir)
	if err != nil {
		return
	}
	d, err := findManifestDigest(&idx, ref)
	if err != nil {
		return
	}
	return s.Image(d.Digest)
}

func (s *Store) Image(id digest.Digest) (r store.Image, err error) {
	manifest, err := s.ImageManifest(id)
	if err != nil {
		return r, fmt.Errorf("image: %s", err)
	}
	f, err := os.Stat(s.blobFile(id))
	if err != nil {
		return r, fmt.Errorf("image: %s", err)
	}
	return store.NewImage(id, "", f.ModTime(), manifest), err
}

func (s *Store) ImageExists(id string) bool {
	_, err := os.Stat(s.imageFile(id))
	return !os.IsNotExist(err)
}

func (s *Store) Images() (r []store.Image, err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("images: %s", err)
		}
	}()
	fl, err := ioutil.ReadDir(s.imageDir)
	if err != nil {
		return
	}
	r = make([]store.Image, 0, len(fl))
	var idx ispecs.Index
	var manifest ispecs.Manifest
	for _, f := range fl {
		if f.IsDir() {
			name := f.Name()
			name, e := s.imageName(name)
			if e == nil {
				idx, e = s.imageIndex(s.imageFile(name))
				if e == nil {
					for _, d := range idx.Manifests {
						ref := d.Annotations[ispecs.AnnotationRefName]
						if ref == "" {
							e = fmt.Errorf("image %q contains manifest descriptor without ref", name, d.Digest)
						} else {
							manifest, e = s.ImageManifest(d.Digest)
							if e == nil {
								r = append(r, store.NewImage(d.Digest, name+":"+ref, f.ModTime(), manifest))
							}
						}
					}
				}
			}
			if e != nil {
				s.err.Printf("warn: image %q: %s", name, e)
			}
		}
	}
	return
}

func (s *Store) DeleteImage(id digest.Digest) (err error) {
	/*imgs, err := s.Images()
	if err != nil {
		return
	}
	for _, img := range imgs {
		if img.ID == id {
			for _, name := range img.Names {
				if e := os.Remove(s.refFile(name)); e != nil {
					err = e
				}
			}
			return
		}
	}
	return fmt.Errorf("Cannot find image %s", id)*/
	panic("TODO: find all images that refer the blob and remove it from them. if the image would be empty afterwards, remove the image dir")
	return nil
}

func (s *Store) CreateImage(name, ref string, manifestDigest digest.Digest) (img store.Image, err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("create image: %s", err)
		}
	}()
	if name == "" {
		return img, fmt.Errorf("create image: no name provided")
	}
	if ref == "" {
		ref = "latest"
	}
	manifest, err := s.ImageManifest(manifestDigest)
	if err != nil {
		return
	}
	manifestFile := s.blobFile(manifestDigest)
	f, err := os.Stat(manifestFile)
	if err != nil {
		return
	}
	descriptor := ispecs.Descriptor{
		MediaType: ispecs.MediaTypeImageManifest,
		Digest:    manifestDigest,
		Size:      f.Size(),
		Annotations: map[string]string{
			ispecs.AnnotationRefName: ref,
		},
		Platform: &ispecs.Platform{
			Architecture: runtime.GOARCH,
			OS:           runtime.GOOS,
		},
	}
	imgDir, err := s.createImageDir(name)
	if err != nil {
		return
	}
	// TODO: lock index.json exclusively while reading/writing
	var idx ispecs.Index
	idxFile := filepath.Join(s.imageFile(name), "index.json")
	if _, e := os.Stat(idxFile); !os.IsNotExist(e) {
		if idx, err = s.imageIndex(idxFile); err != nil {
			return
		}
	}
	idx.SchemaVersion = 2
	if idx.Manifests == nil {
		idx.Manifests = []ispecs.Descriptor{descriptor}
	} else {
		descriptors := make([]ispecs.Descriptor, 1, len(idx.Manifests)+1)
		descriptors[0] = descriptor
		for _, d := range idx.Manifests {
			if d.Annotations[ispecs.AnnotationRefName] != ref || d.Platform.Architecture != runtime.GOARCH || d.Platform.OS != runtime.GOOS {
				descriptors = append(descriptors, d)
			}
		}
	}
	b, err := json.Marshal(&idx)
	if err != nil {
		return img, fmt.Errorf("index: %s", err)
	}
	if err = writeFile(filepath.Join(imgDir, "index.json"), b); err != nil {
		err = fmt.Errorf("write image index.json: %s", err)
		return
	}
	return store.NewImage(manifestDigest, name+":"+ref, time.Now(), manifest), err
}

func (s *Store) ImageGC() error {
	/*// Collect named transitive blobs to leave them untouched
	b := map[digest.Digest]bool{}
	ids := map[string]bool{}
	imgs, err := s.Images()
	if err != nil {
		return err
	}
	for _, img := range imgs {
		ids[img.ID] = true
		manifest, err := s.ImageManifest(img.Manifest.Digest)
		if err != nil {
			return err
		}
		for _, l := range manifest.Layers {
			b[l.Digest] = true
		}
		b[manifest.Config.Digest] = true
		b[img.Manifest.Digest] = true
	}

	// Delete all blobs but the named ones
	ds, err := ioutil.ReadDir(s.blobDir)
	if err != nil {
		return err
	}
	for _, d := range ds {
		if d.IsDir() {
			algDir := filepath.Join(s.blobDir, d.Name())
			fs, err := ioutil.ReadDir(algDir)
			if err != nil {
				return err
			}
			for _, f := range fs {
				blobFile := filepath.Join(algDir, f.Name())
				blobDigest := digest.NewDigestFromHex(d.Name(), f.Name())

				if !b[blobDigest] {
					fmt.Println("Deleting blob " + blobDigest)
					if err = os.Remove(blobFile); err != nil {
						return err
					}
				}
			}
		}
	}

	// Delete all image directories that are not linked
	ds, err = ioutil.ReadDir(s.imageDir)
	if err != nil {
		return err
	}
	for _, d := range ds {
		if !ids[d.Name()] {
			if err = os.RemoveAll(filepath.Join(s.imageDir, d.Name())); err != nil {
				return err
			}
		}
	}*/
	panic("TODO: refactor this")

	return nil
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
		if err = s.unpackImage(*b.ImageManifestDigest, rootfs); err != nil {
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

func (s *Store) unpackImage(manifestDigest digest.Digest, rootfs string) (err error) {
	manifest, err := s.ImageManifest(manifestDigest)
	if err != nil {
		return fmt.Errorf("unpack image: %s", err)
	}
	if err = s.unpackLayers(&manifest, rootfs); err != nil {
		return fmt.Errorf("unpack image: %s", err)
	}
	spec, err := newMtree(rootfs, newFsEval(s.rootless))
	if err != nil {
		return fmt.Errorf("unpack image: %s", err)
	}
	if err = s.writeMtree(manifestDigest, spec); err != nil {
		return fmt.Errorf("unpack image: %s", err)
	}
	return
}

func (s *Store) readMtree(manifestDigest digest.Digest) (*mtree.DirectoryHierarchy, error) {
	mtreeFile := filepath.Join(s.mtreeDir, manifestDigest.Algorithm().String(), manifestDigest.Hex())
	return readMtree(mtreeFile)
}

func (s *Store) writeMtree(manifestDigest digest.Digest, spec *mtree.DirectoryHierarchy) (err error) {
	mtreeFile := filepath.Join(s.mtreeDir, manifestDigest.Algorithm().String(), manifestDigest.Hex())
	if _, e := os.Stat(mtreeFile); os.IsNotExist(e) {
		if err = os.MkdirAll(filepath.Dir(mtreeFile), 0755); err != nil {
			return fmt.Errorf("manifest mtree: %s", err)
		}
		if err = writeMtree(spec, mtreeFile); err != nil {
			return fmt.Errorf("manifest mtree: %s", err)
		}
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

func (s *Store) imageName(fileName string) (name string, err error) {
	b, err := base64.RawStdEncoding.DecodeString(fileName)
	if err == nil {
		fmt.Println("### " + fileName + " ->         " + string(base64.RawStdEncoding.EncodeToString([]byte("docker.io/library/alpine"))))
		name = string(b)
	} else {
		name = fileName
		err = fmt.Errorf("image name: %s", err)
	}
	return
}

func (s *Store) imageFile(name string) string {
	name = base64.RawStdEncoding.EncodeToString([]byte(name))
	return filepath.Join(s.imageDir, name)
}

func (s *Store) blobFile(d digest.Digest) string {
	return filepath.Join(s.blobDir, d.Algorithm().String(), d.Hex())
}

/*func (s *Store) CommitContainer(id string) (img *store.Image, err error) {
	parent, err := s.readParent(id)
	if err != nil {
		return
	}

	reader, err := s.Diff(parentImg.manifestId, id)
	if err != nil {
		return
	}

	// Write tar layer gzip-compressed
	diffIdDigester := digest.SHA256.Digester()
	hashReader := io.TeeReader(reader, diffIdDigester.Hash())
	pipeReader, pipeWriter := io.Pipe()
	defer pipeReader.Close()

	gzw := gzip.NewWriter(pipeWriter)
	defer gzw.Close()
	go func() {
		_, err := io.Copy(gzw, hashReader)
		if err != nil {
			pipeWriter.CloseWithError(fmt.Errorf("compressing layer: %s", err))
			return
		}
		gzw.Close()
		pipeWriter.Close()
	}()

	layerDigest, layerSize, err := s.putBlob(pipeReader)
	if err != nil {
		return
	}

	// Generate new config
	layerDiffID := diffIdDigester.Digest()
	config := &imgspecs.Image{
		RootFS: imgspecs.RootFS{
			DiffIDs: []digest.Digest{layerDiffID},
		},
	}
	confDigest, confSize, err := s.putJsonBlob(config)
	if err != nil {
		return
	}
	m.config.RootFS.DiffIDs = append(m.config.RootFS.DiffIDs, layerDiffID)

	// Generate new manifest
	manifest := &imgspecs.Manifest{
		SchemaVersion: 2,
		Config: imgspecs.Descriptor{
			MediaType: imgspecs.MediaTypeImageConfig,
			Size:      confSize,
			Digest, confDigest,
		},
		Layers: []imgspecs.Descriptor{{
			MediaType: imgspecs.MediaTypeImageLayerGzip,
			Size:      layerSize,
			Digest:    layerDigest,
		}},
	}

	// Create/update image
	if s.ImageExists(id) {
		img, err = s.Image(id)
	} else {
		img, err = s.CreateImage(id)
	}
	if err != nil {
		return fmt.Errorf("commit: %s", err)
	}

	err = writeMtree(spec, mtreeFile)
	return
}*/

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
		parentMtree, err = s.readMtree(*manifestDigest)
		if err != nil {
			return r, fmt.Errorf("commit: %s", err)
		}
	}

	// Diff file system
	rootfs := filepath.Join(s.containerDir, containerId, "rootfs")
	containerMtree, err := newMtree(rootfs, newFsEval(s.rootless))
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
	layer, diffIdDigest, err := s.putLayer(reader)
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
	if err = s.writeMtree(r.Descriptor.Digest, containerMtree); err != nil {
		return r, fmt.Errorf("commit: %s", err)
	}

	// Update container parent
	if err = s.writeParent(containerId, r.Descriptor.Digest); err != nil {
		return r, fmt.Errorf("commit: %s", err)
	}
	return
}

func (s *Store) putLayer(reader io.Reader) (layer ispecs.Descriptor, diffIdDigest digest.Digest, err error) {
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
			pipeWriter.CloseWithError(fmt.Errorf("compressing layer: %s", err))
			return
		}
		gzw.Close()
		pipeWriter.Close()
	}()

	// Write blob
	layer.Digest, layer.Size, err = s.putBlob(pipeReader)
	if err != nil {
		return
	}
	diffIdDigest = diffIdDigester.Digest()
	layer.MediaType = ispecs.MediaTypeImageLayerGzip
	return
}

// Generates a diff tar layer from the container's mtree spec.
func (s *Store) diff(from, to *mtree.DirectoryHierarchy, rootfs string) (io.ReadCloser, error) {
	// Read parent/last mtree
	diffs, err := diffMtree(from, to)
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

func (s *Store) putBlob(reader io.Reader) (d digest.Digest, size int64, err error) {
	// Create temp file to write blob to
	tmpBlob, err := ioutil.TempFile(s.blobDir, "blob-")
	if err != nil {
		err = fmt.Errorf("create temporary blob: %s", err)
		return
	}
	tmpPath := tmpBlob.Name()
	defer tmpBlob.Close()

	// Write temp blob
	digester := digest.SHA256.Digester()
	writer := io.MultiWriter(tmpBlob, digester.Hash())
	if size, err = io.Copy(writer, reader); err != nil {
		err = fmt.Errorf("copy to temporary blob: %s", err)
		return
	}
	tmpBlob.Close()

	// Rename temp blob file
	d = digester.Digest()
	if err = os.Rename(tmpPath, s.blobFile(d)); err != nil {
		err = fmt.Errorf("Cannot rename temp blob file: %s", err)
	}
	return
}

func (s *Store) putJsonBlob(o interface{}) (d digest.Digest, size int64, err error) {
	var buf bytes.Buffer
	if err = json.NewEncoder(&buf).Encode(o); err != nil {
		return
	}
	return s.putBlob(&buf)
}

func (s *Store) createImageDir(name string) (imgDir string, err error) {
	imgDir = s.imageFile(name)
	if err = os.MkdirAll(imgDir, 0755); err != nil {
		return "", fmt.Errorf("create image dir: %s", err)
	}
	imgBlobDir := filepath.Join(imgDir, "blobs")
	if e := os.Symlink(s.blobDir, imgBlobDir); e != nil {
		if _, e = os.Stat(imgBlobDir); os.IsNotExist(e) {
			os.RemoveAll(imgDir)
			return "", fmt.Errorf("create image dir: %s", err)
		} else {
			err = e
		}
	}
	return
}

func (s *Store) unpackLayers(manifest *ispecs.Manifest, dest string) (err error) {
	// Create destination directory
	if err = os.Mkdir(dest, 0755); err != nil {
		return fmt.Errorf("unpack layers: %s", err)
	}
	defer func() {
		if err != nil {
			os.RemoveAll(dest)
		}
	}()

	// Unpack layers
	for _, l := range manifest.Layers {
		d := l.Digest
		s.debug.Printf("Extracting layer %s", d.String())
		layerFile := filepath.Join(s.blobDir, d.Algorithm().String(), d.Hex())
		//unpackLayer(layerFile, dest)
		f, err := os.Open(layerFile)
		if err != nil {
			return fmt.Errorf("unpack layer: %s", err)
		}
		defer f.Close()
		var reader io.Reader
		reader, err = gzip.NewReader(f)
		if err != nil {
			return fmt.Errorf("unpack layer: %s", err)
		}
		if err = layer.UnpackLayer(dest, reader, &layer.MapOptions{Rootless: true}); err != nil {
			return fmt.Errorf("unpack layer: %s", err)
		}
	}

	/*for _, l := range manifest.Layers {
		if l.MediaType != ispecs.MediaTypeImageLayerGzip {
			return fmt.Errorf("Unsupported image layer media type %q in %q", l.MediaType, id)
		}

		if err = unpackLayer(layerFile, dest); err != nil {
			return
		}
	}*/
	return
}

/*func unpackLayer(src, dest string) error {
	f, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("unpack layer: %s", err)
	}
	defer f.Close()
	var reader io.Reader
	reader, err = gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("unpack layer: %s", err)
	}

	header := make([]byte, 10240)
	n, err := reader.Read(header)
	if err != nil && err != io.EOF {
		return fmt.Errorf("unpack layer: %s")
	}
	reader = io.MultiReader(bytes.NewBuffer(header[:n]), reader)
	compression := archive.DetectCompression(header[:n])
	_, err = archive.UnpackLayer(dest, reader, &archive.TarOptions{
		Compression: compression,
		UIDMaps:     []idtools.IDMap{{ContainerID: 0, HostID: os.Geteuid(), Size: 1}},
		GIDMaps:     []idtools.IDMap{{ContainerID: 0, HostID: os.Getegid(), Size: 1}},
		NoLchown:    true,
		ChownOpts: &archive.TarChownOptions{
			UID: os.Geteuid(),
			GID: os.Getegid(),
		},
		IncludeSourceDir: true,
		WhiteoutFormat:   archive.AUFSWhiteoutFormat,
	})
	if err != nil {
		return fmt.Errorf("unpack layer: %s", err)
	}
	return nil
}*/

func (s *Store) imageIndex(dir string) (r ispecs.Index, err error) {
	b, err := ioutil.ReadFile(filepath.Join(dir, "index.json"))
	if err != nil {
		return r, fmt.Errorf("read image index: %s", err)
	}
	if err = json.Unmarshal(b, &r); err != nil {
		err = fmt.Errorf("unmarshal image %q index: %s", dir, err)
	}
	return
}

func (s *Store) ImageManifest(manifestDigest digest.Digest) (r ispecs.Manifest, err error) {
	b, err := s.readBlob(manifestDigest)
	if err != nil {
		return r, fmt.Errorf("image manifest: %s", err)
	}
	if err = json.Unmarshal(b, &r); err != nil {
		err = fmt.Errorf("unmarshal image manifest %q: %s", manifestDigest.String(), err)
	}
	return
}

func (s *Store) PutImageManifest(m ispecs.Manifest) (d ispecs.Descriptor, err error) {
	d.Digest, d.Size, err = s.putJsonBlob(m)
	d.MediaType = ispecs.MediaTypeImageManifest
	d.Platform = &ispecs.Platform{
		Architecture: runtime.GOARCH,
		OS:           runtime.GOOS,
	}
	if err != nil {
		err = fmt.Errorf("put image manifest: %s", err)
	}
	return
}

func (s *Store) ImageConfig(configDigest digest.Digest) (r ispecs.Image, err error) {
	b, err := s.readBlob(configDigest)
	if err != nil {
		return r, fmt.Errorf("image config: %s", err)
	}
	if err = json.Unmarshal(b, &r); err != nil {
		err = fmt.Errorf("unmarshal image config: %s", err)
	}
	return
}

func (s *Store) PutImageConfig(m ispecs.Image) (d ispecs.Descriptor, err error) {
	d.Digest, d.Size, err = s.putJsonBlob(m)
	d.MediaType = ispecs.MediaTypeImageConfig
	if err != nil {
		err = fmt.Errorf("put image config: %s", err)
	}
	return
}

func (s *Store) readBlob(id digest.Digest) (b []byte, err error) {
	if err = id.Validate(); err != nil {
		return nil, fmt.Errorf("blob digest %q: %s", id.String(), err)
	}
	b, err = ioutil.ReadFile(filepath.Join(s.blobDir, id.Algorithm().String(), id.Hex()))
	if err != nil {
		err = fmt.Errorf("read blob %s: %s", id, err)
	}
	return
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

func createTrustPolicyContext() (*signature.PolicyContext, error) {
	policyFile := ""
	var policy *signature.Policy // This could be cached across calls, if we had an application context.
	var err error
	//if insecurePolicy {
	//	policy = &signature.Policy{Default: []signature.PolicyRequirement{signature.NewPRInsecureAcceptAnything()}}
	if policyFile == "" {
		policy, err = signature.DefaultPolicy(nil)
	} else {
		policy, err = signature.NewPolicyFromFile(policyFile)
	}
	if err != nil {
		return nil, err
	}
	return signature.NewPolicyContext(policy)
}
