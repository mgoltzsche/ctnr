package oci

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/containers/image/copy"
	ocitransport "github.com/containers/image/oci/layout"
	"github.com/containers/image/signature"
	"github.com/containers/image/transports/alltransports"
	"github.com/containers/image/types"
	"github.com/mgoltzsche/cntnr/log"
	"github.com/mgoltzsche/cntnr/pkg/atomic"
	lock "github.com/mgoltzsche/cntnr/pkg/lock"
	"github.com/mgoltzsche/cntnr/store"
	digest "github.com/opencontainers/go-digest"
	ispecs "github.com/opencontainers/image-spec/specs-go/v1"
)

const (
	AnnotationImported = "com.github.mgoltzsche.cntnr.image.imported"
)

type ImageStoreImpl struct {
	*BlobStoreExt
	imageDir      string
	systemContext *types.SystemContext
	trustPolicy   *signature.PolicyContext
	lock          lock.SharedLock
	err           log.Logger
}

func NewImageStore(dir string, blobStore *BlobStoreExt, systemContext *types.SystemContext, errorLog log.Logger) (r *ImageStoreImpl, err error) {
	if err = os.MkdirAll(dir, 0755); err != nil {
		err = fmt.Errorf("init image store: %s", err)
		return
	}
	trustPolicy, err := createTrustPolicyContext()
	if err != nil {
		return
	}
	lck, err := lock.NewSharedLock(filepath.Join(os.TempDir(), "cntnr", "lock"))
	return &ImageStoreImpl{blobStore, dir, systemContext, trustPolicy, &lck, errorLog}, err
}

// Creates a new image. Overwrites existing refs.
func (s *ImageStoreImpl) CreateImage(name, ref string, manifestDigest digest.Digest) (img store.Image, err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("create image: %s", err)
		}
	}()

	if name == "" {
		err = fmt.Errorf("no name provided")
		return
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
	manifestDescriptor := ispecs.Descriptor{
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

	// Create/update index.json
	if err = s.addImage(name, []ispecs.Descriptor{manifestDescriptor}); err != nil {
		return
	}

	return store.NewImage(manifestDigest, name, ref, time.Now(), manifest, nil, s), err
}

func (s *ImageStoreImpl) ImageByName(nameRef string) (r store.Image, err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("image %q not found in local store: %s", nameRef, err)
		}
	}()
	if id, e := digest.Parse(nameRef); e == nil && id.Validate() == nil {
		return s.Image(id)
	}
	name := nameRef
	ref := "latest"
	if imgRef, e := alltransports.ParseImageName(name); e == nil {
		name, ref = nameAndRef(imgRef)
	}
	var idx ispecs.Index
	if err = imageIndex(s.name2dir(name), &idx); err != nil {
		return
	}
	d, err := findManifestDigest(&idx, ref)
	if err != nil {
		return
	}
	return s.Image(d.Digest)
}

func (s *ImageStoreImpl) Image(id digest.Digest) (r store.Image, err error) {
	manifest, err := s.ImageManifest(id)
	if err != nil {
		return r, fmt.Errorf("image: %s", err)
	}
	f, err := os.Stat(s.blobFile(id))
	if err != nil {
		return r, fmt.Errorf("image: %s", err)
	}
	return store.NewImage(id, "", "", f.ModTime(), manifest, nil, s), err
}

func (s *ImageStoreImpl) Images() (r []store.Image, err error) {
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
			name, e := s.dir2name(name)
			if e == nil {
				if e = imageIndex(s.name2dir(name), &idx); e == nil {
					for _, d := range idx.Manifests {
						ref := d.Annotations[ispecs.AnnotationRefName]
						if ref == "" {
							e = fmt.Errorf("image %q contains manifest descriptor without ref", name, d.Digest)
						} else {
							manifest, e = s.ImageManifest(d.Digest)
							if e == nil {
								r = append(r, store.NewImage(d.Digest, name, ref, f.ModTime(), manifest, nil, s))
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

func (s *ImageStoreImpl) DeleteImage(name, ref string) (err error) {
	if imgRef, e := alltransports.ParseImageName(name); e == nil {
		name, _ = nameAndRef(imgRef)
	}
	if _, e := os.Stat(s.name2dir(name)); os.IsNotExist(e) {
		return fmt.Errorf("image %q does not exist", name)
	}
	err = s.updateImageIndex(name, func(idx *ispecs.Index) error {
		if ref == "" {
			idx.Manifests = nil
		} else {
			manifests := make([]ispecs.Descriptor, 0, len(idx.Manifests))
			deleted := false
			for _, m := range idx.Manifests {
				if ref == m.Annotations[ispecs.AnnotationRefName] {
					deleted = true
				} else {
					manifests = append(manifests, m)
				}
			}
			idx.Manifests = manifests
			if !deleted {
				return fmt.Errorf("image %q has no ref %q", name, ref)
			}
		}
		return nil
	})
	if err != nil {
		err = fmt.Errorf("delete image: %s", err)
	}
	return err
}

func (s *ImageStoreImpl) CommitImage(rootfs, name string, parentManifest *digest.Digest, author, comment string) (r store.Image, err error) {
	c, err := s.CommitLayer(rootfs, parentManifest, author, comment)
	if err != nil {
		return r, fmt.Errorf("commit image: %s", err)
	}
	name, ref := parseImageName(name)
	if name != "" {
		if r, err = s.CreateImage(name, ref, c.Descriptor.Digest); err == nil {
			err = fmt.Errorf("commit image: %s", err)
		}
	}
	r = store.NewImage(c.Descriptor.Digest, name, ref, time.Now(), c.Manifest, &c.Config, s)
	return
}

func (s *ImageStoreImpl) ImageGC() (err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("image gc: %s", err)
		}
	}()

	if err = s.lock.Lock(); err != nil {
		return
	}
	defer s.unlock()

	// Collect named transitive blobs to leave them untouched
	keep := map[digest.Digest]bool{}
	imgs, err := s.Images()
	if err != nil {
		return err
	}
	for _, img := range imgs {
		keep[img.ID] = true
		keep[img.Manifest.Config.Digest] = true
		for _, l := range img.Manifest.Layers {
			keep[l.Digest] = true
		}
	}

	// Delete all but the named blobs
	return s.RetainBlobs(keep)
}

func (s *ImageStoreImpl) unlock() {
	if err := s.lock.Unlock(); err != nil {
		fmt.Fprint(os.Stderr, "Error: %s", err)
	}
}

func (s *ImageStoreImpl) ImportImage(src string) (img store.Image, err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("import: %s", err)
		}
	}()

	// Parse source
	srcRef, err := alltransports.ParseImageName(src)
	if err != nil {
		err = fmt.Errorf("invalid source %q: %s", src, err)
		return
	}

	// Create temp image directory
	name, ref := nameAndRef(srcRef)
	imgDir, err := ioutil.TempDir(s.imageDir, "tmpimg-")
	if err != nil {
		return
	}
	defer os.RemoveAll(imgDir)
	imgBlobDir := filepath.Join(imgDir, "blobs")
	if err = os.Symlink(s.blobDir, imgBlobDir); err != nil {
		return
	}

	// Parse destination
	destRef, err := ocitransport.Transport.ParseReference(imgDir + ":" + ref)
	if err != nil {
		err = fmt.Errorf("invalid destination %q: %s", imgDir, err)
		return
	}

	// Copy image
	err = copy.Image(s.trustPolicy, destRef, srcRef, &copy.Options{
		RemoveSignatures: false,
		SignBy:           "",
		ReportWriter:     os.Stdout,
		SourceCtx:        s.systemContext,
		DestinationCtx:   &types.SystemContext{},
	})
	if err != nil {
		return
	}

	// Add manifests to store's index
	var idx ispecs.Index
	if err = imageIndex(imgDir, &idx); err != nil {
		return
	}
	for _, m := range idx.Manifests {
		m.Annotations[AnnotationImported] = "true"
	}
	if err = s.addImage(name, idx.Manifests); err != nil {
		return
	}
	return s.ImageByName(src)
}

func (s *ImageStoreImpl) Close() (err error) {
	if err = s.lock.Close(); err != nil {
		err = fmt.Errorf("close store: %s", err)
	}
	return
}

// Adds manifests to an image using a file lock
func (s *ImageStoreImpl) addImage(name string, manifestDescriptors []ispecs.Descriptor) error {
	if len(manifestDescriptors) == 0 {
		return nil
	}
	for _, manifestDescriptor := range manifestDescriptors {
		if manifestDescriptor.Annotations[ispecs.AnnotationRefName] == "" {
			return fmt.Errorf("no image ref defined in manifest descriptor (%s annotation)", ispecs.AnnotationRefName)
		}
		if manifestDescriptor.Digest == digest.Digest("") || manifestDescriptor.Size < 1 || manifestDescriptor.Platform.Architecture == "" || manifestDescriptor.Platform.OS == "" {
			str := ""
			if b, e := json.Marshal(&manifestDescriptor); e == nil {
				str = string(b)
			}
			return fmt.Errorf("incomplete manifest descriptor %s", str)
		}
	}
	return s.updateImageIndex(name, func(idx *ispecs.Index) error {
		addManifests(&idx.Manifests, manifestDescriptors)
		return nil
	})
}

func (s *ImageStoreImpl) updateImageIndex(name string, transform func(idx *ispecs.Index) error) error {
	return UpdateImageIndex(s.name2dir(name), s.blobDir, transform)
}

func (s *ImageStoreImpl) name2dir(name string) string {
	name = base64.RawStdEncoding.EncodeToString([]byte(name))
	return filepath.Join(s.imageDir, name)
}

func (s *ImageStoreImpl) dir2name(fileName string) (name string, err error) {
	b, err := base64.RawStdEncoding.DecodeString(fileName)
	if err == nil {
		name = string(b)
	} else {
		name = fileName
		err = fmt.Errorf("image name: %s", err)
	}
	return
}

// Updates an image index using a file lock
func UpdateImageIndex(imgDir, externalBlobDir string, transform func(*ispecs.Index) error) (err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("update image index %s/index.json: %s", imgDir, err)
		}
	}()

	if externalBlobDir != "" && !filepath.IsAbs(externalBlobDir) {
		return fmt.Errorf("image index: externalBlobDir is not an absolute path: %q", externalBlobDir)
	}
	// TODO: Use tmpfs
	l, err := lock.LockFile(filepath.Clean(imgDir) + ".lock")
	if err != nil {
		return
	}

	// Lock image directory
	if err = l.Lock(); err != nil {
		return
	}
	defer func() {
		if e := l.Unlock(); e != nil && err == nil {
			err = e
		}
	}()

	// Create image directory if not exists
	dirExisted := true
	if _, e := os.Stat(imgDir); os.IsNotExist(e) {
		dirExisted = false
		if err = os.Mkdir(imgDir, 0755); err != nil {
			return
		}
	}
	defer func() {
		if err != nil && !dirExisted {
			os.RemoveAll(imgDir)
		}
	}()

	// Create/link blob dir if not exists
	blobDir := filepath.Join(imgDir, "blobs")
	if externalBlobDir == "" {
		if err = os.MkdirAll(blobDir, 0755); err != nil {
			return
		}
	} else if _, e := os.Lstat(blobDir); os.IsNotExist(e) {
		if err = os.Symlink(externalBlobDir, blobDir); err != nil {
			return
		}
	}

	// Create/check oci-layout
	layoutFile := filepath.Join(imgDir, "oci-layout")
	if _, e := os.Stat(layoutFile); os.IsNotExist(e) {
		// Create new oci-layout file
		layout := ispecs.ImageLayout{}
		layout.Version = ispecs.ImageLayoutVersion
		if _, err = atomic.WriteJson(layoutFile, &layout); err != nil {
			return
		}
	} else {
		// Check existing layout's version
		layout := ispecs.ImageLayout{}
		b, err := ioutil.ReadFile(layoutFile)
		if err != nil {
			return fmt.Errorf("read oci-layout: %s", err)
		}
		if err = json.Unmarshal(b, &layout); err != nil {
			return fmt.Errorf("unmarshal oci-layout: %s", err)
		}
		if layout.Version != ispecs.ImageLayoutVersion {
			return fmt.Errorf("unsupported oci-layout version %q", layout.Version)
		}
	}

	// Create/load index.json
	idxFile := filepath.Join(imgDir, "index.json")
	var idx ispecs.Index
	if _, e := os.Stat(idxFile); os.IsNotExist(e) {
		idx.SchemaVersion = 2
	} else {
		if err = imageIndex(imgDir, &idx); err != nil {
			return
		}
		if idx.SchemaVersion != 2 {
			return fmt.Errorf("unsupported index schema version %d in %s", idx.SchemaVersion, idxFile)
		}
	}

	// Transform image index
	if err = transform(&idx); err != nil {
		return
	}

	// Delete whole image repository (dir) if manifest list is empty
	if len(idx.Manifests) == 0 {
		if e := os.Remove(idxFile); e != nil {
			if _, es := os.Stat(idxFile); !os.IsNotExist(es) {
				return e
			}
		}
		return os.RemoveAll(imgDir)
	}

	// Write modified index.json
	_, err = atomic.WriteJson(idxFile, &idx)
	return
}

func imageIndex(dir string, r *ispecs.Index) error {
	idxFile := filepath.Join(dir, "index.json")
	b, err := ioutil.ReadFile(idxFile)
	if err != nil {
		return fmt.Errorf("read image index: %s", err)
	}
	if err = json.Unmarshal(b, r); err != nil {
		return fmt.Errorf("unmarshal image index %s: %s", idxFile, err)
	}
	return nil
}

func addManifests(manifests *[]ispecs.Descriptor, addAll []ispecs.Descriptor) {
	if len(*manifests) == 0 {
		*manifests = addAll
	} else {
		filtered := make([]ispecs.Descriptor, 0, len(*manifests)+len(addAll))
		filtered = append(filtered, addAll...)
		for _, m := range *manifests {
			mref := m.Annotations[ispecs.AnnotationRefName]
			exists := false
			for _, add := range addAll {
				if mref == add.Annotations[ispecs.AnnotationRefName] && m.Platform.Architecture == add.Platform.Architecture && m.Platform.OS == add.Platform.OS {
					exists = true
				}
			}
			if !exists {
				filtered = append(filtered, m)
			}
		}
		*manifests = filtered
	}
}

func parseNameAndRef(nameAndTag string) (name, ref string, err error) {
	imgRef, err := alltransports.ParseImageName(nameAndTag)
	if err != nil {
		return
	}
	name, ref = nameAndRef(imgRef)
	return
}

func nameAndRef(imgRef types.ImageReference) (string, string) {
	name := strings.TrimLeft(imgRef.StringWithinTransport(), "/")
	dckrRef := imgRef.DockerReference()
	if dckrRef != nil {
		name = dckrRef.String()
	}
	return parseImageName(name)
}

func parseImageName(nameAndRef string) (name, ref string) {
	if li := strings.LastIndex(nameAndRef, ":"); li > 0 && li+1 < len(nameAndRef) {
		ref = nameAndRef[li+1:]
		name = nameAndRef[:li]
	} else {
		ref = "latest"
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
