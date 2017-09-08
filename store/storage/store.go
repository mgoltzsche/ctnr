package storage

import (
	"fmt"

	"encoding/json"
	"os"
	"path/filepath"

	"bytes"
	"io/ioutil"

	"github.com/containers/image/copy"
	"github.com/containers/image/signature"
	storetransport "github.com/containers/image/storage"
	"github.com/containers/image/transports/alltransports"
	"github.com/containers/image/types"
	"github.com/containers/storage"
	//"github.com/containers/storage/pkg/idtools"
	"github.com/mgoltzsche/cntnr/store"
	imgspecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/opencontainers/runtime-tools/generate"
)

type Store struct {
	store         storage.Store
	trustPolicy   *signature.PolicyContext
	systemContext *types.SystemContext
}

func NewContainersStore(dir string, systemContext *types.SystemContext) (*Store, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}

	opts := storage.DefaultStoreOptions
	opts.GraphDriverName = "overlay"
	/*opts.UIDMap = []idtools.IDMap{{HostID: os.Geteuid(), ContainerID: 0, Size: 1}}
	opts.GIDMap = []idtools.IDMap{{HostID: os.Getegid(), ContainerID: 0, Size: 1}}*/
	opts.RunRoot = fmt.Sprintf("/run/user/%d/cntnr/containers-storage", os.Geteuid())
	opts.GraphRoot = dir
	store, err := storage.GetStore(opts)
	if err != nil {
		return nil, fmt.Errorf("Cannot open store at %s: %s", dir, err)
	}

	trustPolicy, err := createTrustPolicyContext()
	if err != nil {
		return nil, fmt.Errorf("Error loading trust policy: %s", err)
	}

	return &Store{store, trustPolicy, systemContext}, nil
}

func (s *Store) Close() error {
	_, err := s.store.Shutdown(true)
	return err
}

func (s *Store) ImportImage(src string) (img *store.Image, err error) {
	srcRef, err := alltransports.ParseImageName(src)
	if err != nil {
		err = fmt.Errorf("Invalid image source %q: %s", src, err)
		return
	}
	imageName := store.ToName(srcRef)
	// TODO: maybe use srcRef.NewImage(ctx) ... to get actual image ID to add as @suffix to destRef before copying.
	// Problem: to much code copy.Image code to rewrite or image metadata is fetched twice.
	destRef, err := storetransport.Transport.ParseStoreReference(s.store, imageName)
	if err != nil {
		err = fmt.Errorf("Invalid image import destination %q: %s", imageName, err)
		return
	}

	err = copy.Image(s.trustPolicy, destRef, srcRef, &copy.Options{
		RemoveSignatures: false,
		SignBy:           "",
		ReportWriter:     os.Stdout,
		SourceCtx:        s.systemContext,
		DestinationCtx:   &types.SystemContext{},
	})
	fmt.Println()
	if err != nil {
		return
	}
	// TODO: generate unique ID and pass it to copy method to be able to return ID here
	return s.ImageByName(src)
}

func (s *Store) Image(id string) (r *store.Image, err error) {
	img, err := s.store.Image(id)
	if err == nil {
		r = store.NewImage(img.ID, img.Names, img.Created)
	}
	return
}

func (s *Store) ImageByName(name string) (r *store.Image, err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("Cannot find image %q in the local store: %s", name, err)
		}
	}()
	if imgRef, err := alltransports.ParseImageName(name); err == nil {
		name = store.ToName(imgRef)
	}
	id, err := s.store.Lookup(name)
	if err != nil {
		return
	}
	img, err := s.store.Image(id)
	if err != nil {
		return
	}
	r = store.NewImage(img.ID, img.Names, img.Created)
	return
}

func (s *Store) Images() (r []*store.Image, err error) {
	imgs, err := s.store.Images()
	if err == nil {
		r = make([]*store.Image, len(imgs))
		for i, img := range imgs {
			r[i] = store.NewImage(img.ID, img.Names, img.Created)
		}
	}
	return
}

func (s *Store) DeleteImage(id string) error {
	_, err := s.store.DeleteImage(id, true)
	return err
}

func (s *Store) ImageGC() error {
	panic("Not supported on containers/storage store")
	return nil
}

func (s *Store) CreateContainer(id string, spec *generate.Generator, imageId string) (r *store.Container, err error) {
	c, err := s.store.CreateContainer(id, []string{}, imageId, "", "", nil)
	if err != nil {
		return
	}
	defer func() {
		if err != nil {
			err = fmt.Errorf("Cannot create container: %s", err)
			s.store.DeleteContainer(c.ID)
		}
	}()
	dir, err := s.store.Mount(c.ID, "")
	if err != nil {
		return
	}
	defer func() {
		if err != nil {
			s.store.Unmount(c.ID)
		}
	}()

	// Write config.json
	var buf bytes.Buffer
	bundleDir := filepath.Join(dir, "..")
	spec.SetRootPath(filepath.Base(dir))
	if err = spec.Save(&buf, generate.ExportOptions{Seccomp: false}); err != nil {
		return
	}
	s.store.SetContainerBigData(c.ID, "config.json", buf.Bytes())
	err = ioutil.WriteFile(filepath.Join(bundleDir, "config.json"), buf.Bytes(), 0640)

	return store.NewContainer(c.ID, bundleDir), err
}

func (s *Store) ImageConfig(imageId string) (r *imgspecs.Image, err error) {
	m, err := s.imageManifest(imageId)
	if err != nil {
		return
	}
	r = &imgspecs.Image{}
	b, err := s.store.ImageBigData(imageId, m.Config.Digest.String())
	if err != nil {
		return
	}
	if err = json.Unmarshal(b, r); err != nil {
		err = fmt.Errorf("Cannot read image %q spec: %s", imageId, err)
	}
	return
}

func (s *Store) imageManifest(imageId string) (m *imgspecs.Manifest, err error) {
	mj, err := s.store.ImageBigData(imageId, "manifest")
	if err != nil {
		return
	}
	m = &imgspecs.Manifest{}
	if err = json.Unmarshal(mj, m); err != nil {
		err = fmt.Errorf("Cannot read image %q manifest: %s", imageId, err)
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
