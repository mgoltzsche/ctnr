package oci

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"encoding/base64"

	"runtime"

	"github.com/containers/image/copy"
	ocitransport "github.com/containers/image/oci/layout"
	"github.com/containers/image/signature"
	"github.com/containers/image/transports/alltransports"
	"github.com/containers/image/types"
	"github.com/mgoltzsche/cntnr/log"
	"github.com/mgoltzsche/cntnr/store"
	digest "github.com/opencontainers/go-digest"
	imgspecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/opencontainers/runtime-tools/generate"
)

type Store struct {
	containerDir  string
	refDir        string
	imageDir      string
	blobDir       string
	systemContext *types.SystemContext
	trustPolicy   *signature.PolicyContext
	debug         log.Logger
}

func NewSimpleStore(dir string, systemContext *types.SystemContext, debug log.Logger) (s *Store, err error) {
	dir, err = filepath.Abs(dir)
	if err != nil {
		return
	}
	refDir := filepath.Join(dir, "refs")
	imageDir := filepath.Join(dir, "images")
	blobDir := filepath.Join(dir, "blobs")
	containerDir := filepath.Join(dir, "containers")
	for _, sdir := range []string{blobDir, imageDir, refDir, containerDir} {
		if err = os.MkdirAll(sdir, 0755); err != nil {
			return
		}
	}
	trustPolicy, err := createTrustPolicyContext()
	if err != nil {
		return
	}
	return &Store{containerDir, refDir, imageDir, blobDir, systemContext, trustPolicy, debug}, nil
}

func (s *Store) Close() error {
	return nil
}

func (s *Store) ImportImage(src string) (img *store.Image, err error) {
	srcRef, err := alltransports.ParseImageName(src)
	if err != nil {
		err = fmt.Errorf("Invalid image source %s: %s", src, err)
		return
	}

	// Create image directory under new image UUID
	id := store.GenerateId()
	imageDir := filepath.Join(s.imageDir, id)
	if err = os.Mkdir(imageDir, 0750); err != nil {
		return
	}
	defer func() {
		if err != nil {
			os.RemoveAll(imageDir)
		}
	}()
	if err = os.Symlink(s.blobDir, filepath.Join(imageDir, "blobs")); err != nil {
		return
	}

	// Copy image
	destRef, err := ocitransport.Transport.ParseReference(imageDir)
	if err != nil {
		err = fmt.Errorf("Invalid image import destination %q: %s", imageDir, err)
		return
	}
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

	img, err = s.Image(id)
	if err != nil {
		return
	}

	err = s.setImageRef(store.ToName(srcRef), id)
	return
}

func (s *Store) ImageByName(name string) (r *store.Image, err error) {
	if imgRef, err := alltransports.ParseImageName(name); err == nil {
		name = store.ToName(imgRef)
	}
	refFile := s.refFile(name)
	f, err := os.Lstat(refFile)
	if err != nil || f.Mode()&os.ModeSymlink == 0 {
		return nil, fmt.Errorf("Image %q not found in local store: %s", name, err)
	}
	imageDir, err := os.Readlink(refFile)
	if err != nil {
		return nil, fmt.Errorf("Image %q not found in local store: %s", name, err)
	}
	return s.Image(filepath.Base(imageDir))
}

func (s *Store) Image(id string) (r *store.Image, err error) {
	st, err := os.Stat(filepath.Join(s.imageDir, id))
	if err != nil {
		return nil, fmt.Errorf("Image %q not found in local store: %s", id, err)
	}
	return store.NewImage(id, []string{}, st.ModTime()), nil
}

func (s *Store) Images() (r []*store.Image, err error) {
	fs, err := ioutil.ReadDir(s.refDir)
	if err != nil {
		return nil, err
	}
	r = make([]*store.Image, 0, len(fs))
	for _, f := range fs {
		if f.Mode()&os.ModeSymlink != 0 {
			name := s.refName(f.Name())
			imageDir, e := os.Readlink(filepath.Join(s.refDir, f.Name()))
			if e == nil {
				id := filepath.Base(imageDir)
				// TODO: add creation date and size
				r = append(r, store.NewImage(id, []string{name}, f.ModTime()))
			} else {
				err = e
			}
		}
	}
	return
}

func (s *Store) DeleteImage(id string) (err error) {
	imgs, err := s.Images()
	if err != nil {
		return
	}
	for _, img := range imgs {
		if img.ID() == id {
			for _, name := range img.Names() {
				if e := os.Remove(s.refFile(name)); e != nil {
					err = e
				}
			}
			return
		}
	}
	return fmt.Errorf("Cannot find image %s", id)
}

func (s *Store) ImageGC() error {
	// Collect named transitive blobs to leave them untouched
	b := map[digest.Digest]bool{}
	ids := map[string]bool{}
	imgs, err := s.Images()
	if err != nil {
		return err
	}
	for _, img := range imgs {
		ids[img.ID()] = true
		manifest, d, err := s.imageManifest(img.ID())
		if err != nil {
			return err
		}
		for _, l := range manifest.Layers {
			b[l.Digest] = true
		}
		b[manifest.Config.Digest] = true
		b[d] = true
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
	}

	return nil
}

func (s *Store) CreateContainer(id string, spec *generate.Generator, imageId string) (c *store.Container, err error) {
	if id == "" {
		id = store.GenerateId()
	}
	dir := filepath.Join(s.containerDir, id)
	rootfs := filepath.Join(dir, "rootfs")
	if err = os.Mkdir(dir, 0770); err != nil {
		err = fmt.Errorf("Cannot create container: %s", err)
		return
	}
	defer func() {
		if err != nil {
			os.RemoveAll(dir)
		}
	}()

	// Extract image
	if imageId != "" {
		s.UnpackImage(imageId, rootfs)
	}

	// Write config.json
	cfgFile := filepath.Join(dir, "config.json")
	spec.SetRootPath("rootfs")
	err = spec.SaveToFile(cfgFile, generate.ExportOptions{Seccomp: false})

	return store.NewContainer(id, dir), err
}

func (s *Store) UnpackImage(id, dest string) (err error) {
	var manifest *imgspecs.Manifest
	if manifest, _, err = s.imageManifest(id); err != nil {
		return err
	}
	if err = os.Mkdir(dest, 0770); err != nil {
		return fmt.Errorf("Cannot unpack image %q: %s", id, err)
	}
	defer func() {
		if err != nil {
			os.RemoveAll(dest)
		}
	}()

	for _, l := range manifest.Layers {
		if l.MediaType != imgspecs.MediaTypeImageLayerGzip {
			return fmt.Errorf("Unsupported layer media type %q", l.MediaType)
		}

		layerFile := filepath.Join(s.blobDir, l.Digest.Algorithm().String(), l.Digest.Hex())
		s.debug.Printf("Extracting layer %s", l.Digest.String())

		if err = unpackLayer(layerFile, dest); err != nil {
			return
		}
	}
	return
}

func (s *Store) ImageConfig(imageId string) (r *imgspecs.Image, err error) {
	manifest, _, err := s.imageManifest(imageId)
	if err != nil {
		return
	}
	b, err := s.readBlob(manifest.Config.Digest)
	if err != nil {
		return
	}
	r = &imgspecs.Image{}
	if err = json.Unmarshal(b, r); err != nil {
		err = fmt.Errorf("Cannot unmarshal image %q config: %s", imageId, err)
	}
	return
}

func (s *Store) imageIndex(imageId string) (r *imgspecs.Index, err error) {
	b, err := ioutil.ReadFile(filepath.Join(s.imageDir, imageId, "index.json"))
	if err != nil {
		return nil, fmt.Errorf("Cannot read image %q index: %s", imageId, err)
	}
	r = &imgspecs.Index{}
	if err = json.Unmarshal(b, r); err != nil {
		err = fmt.Errorf("Cannot unmarshal image %q index: %s", imageId, err)
	}
	return
}

func (s *Store) imageManifest(imageId string) (m *imgspecs.Manifest, d digest.Digest, err error) {
	idx, err := s.imageIndex(imageId)
	if err != nil {
		return
	}
	d, err = findManifestDigest(idx)
	if err != nil {
		return
	}
	b, err := s.readBlob(d)
	if err != nil {
		return
	}
	m = &imgspecs.Manifest{}
	if err = json.Unmarshal(b, m); err != nil {
		err = fmt.Errorf("Cannot unmarshal image %q manifest %s: %s", imageId, d, err)
	}
	return
}

func (s *Store) readBlob(id digest.Digest) (b []byte, err error) {
	b, err = ioutil.ReadFile(filepath.Join(s.blobDir, id.Algorithm().String(), id.Hex()))
	if err != nil {
		err = fmt.Errorf("Cannot read blob %s: %s", id, err)
	}
	return
}

func (s *Store) setImageRef(name string, id string) error {
	refFile := s.refFile(name)
	imageDir := filepath.Join(s.imageDir, id)
	if _, err := os.Stat(imageDir); err != nil {
		return fmt.Errorf("Cannot set image ref %q since image ID %q cannot be resolved: %s", name, id, err)
	}
	return os.Symlink(imageDir, refFile)
}

func (s *Store) refFile(name string) string {
	name = base64.RawStdEncoding.EncodeToString([]byte(name))
	return filepath.Join(s.refDir, name)
}

func (s *Store) refName(refFile string) string {
	name, err := base64.RawStdEncoding.DecodeString(refFile)
	if err != nil {
		panic(fmt.Sprintf("Unsupported image ref file name %q: %s\n", refFile, err))
	}
	return string(name)
}

func findManifestDigest(idx *imgspecs.Index) (d digest.Digest, err error) {
	for _, ref := range idx.Manifests {
		if ref.Platform.Architecture == runtime.GOARCH && ref.Platform.OS == runtime.GOOS {
			return ref.Digest, nil
		}
	}
	err = fmt.Errorf("No image manifest for platform architecture %s and OS %s!", runtime.GOARCH, runtime.GOOS)
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
