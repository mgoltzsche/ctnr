package storage

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/containers/image/copy"
	"github.com/containers/image/signature"
	storetransport "github.com/containers/image/storage"
	"github.com/containers/image/transports/alltransports"
	"github.com/containers/image/types"
	"github.com/containers/storage"
	"github.com/containers/storage/pkg/archive"
	"github.com/containers/storage/pkg/idtools"
	"github.com/mgoltzsche/cntnr/store"
	ispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
)

// Deprecated since containers/storage cannot be used by
// unprivileged user (https://github.com/containers/storage/issues/96)
type Store struct {
	store         storage.Store
	trustPolicy   *signature.PolicyContext
	systemContext *types.SystemContext
}

var _ store.Store = &Store{}

func NewContainersStore(dir string, systemContext *types.SystemContext) (*Store, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}

	opts := storage.DefaultStoreOptions
	opts.GraphDriverName = "overlay"
	opts.UIDMap = []idtools.IDMap{{HostID: os.Geteuid(), ContainerID: 0, Size: 1}}
	opts.GIDMap = []idtools.IDMap{{HostID: os.Getegid(), ContainerID: 0, Size: 1}}
	opts.RunRoot = fmt.Sprintf("/run/user/%d/cntnr/containers-storage", os.Geteuid())
	opts.GraphRoot = dir
	store, err := storage.GetStore(opts)
	if err != nil {
		return nil, errors.Wrap(err, "open store at "+dir)
	}

	trustPolicy, err := createTrustPolicyContext()
	if err != nil {
		return nil, errors.Wrap(err, "loading trust policy")
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
		err = errors.Wrapf(err, "invalid image source %q", src)
		return
	}
	imageName := store.ToName(srcRef)
	// TODO: maybe use srcRef.NewImage(ctx) ... to get actual image ID to add as @suffix to destRef before copying.
	// Problem: to much code copy.Image code to rewrite or image metadata is fetched twice.
	destRef, err := storetransport.Transport.ParseStoreReference(s.store, imageName)
	if err != nil {
		err = errors.Wrapf(err, "invalid image import destination %q", imageName)
		return
	}

	reportWriter := os.Stdout
	err = copy.Image(s.trustPolicy, destRef, srcRef, &copy.Options{
		RemoveSignatures: false,
		SignBy:           "",
		ReportWriter:     reportWriter,
		SourceCtx:        s.systemContext,
		DestinationCtx:   &types.SystemContext{},
	})
	reportWriter.WriteString("\n")
	if err != nil {
		return
	}
	// TODO: generate unique ID and pass it to copy method to be able to return ID here
	return s.ImageByName(src)
}

func (s *Store) CreateImage(id string, names []string, layerId string, cfg *ispecs.Image) (*store.Image, error) {
	now := time.Now()
	img, err := s.store.CreateImage(id, names, layerId, "", &storage.ImageOptions{CreationDate: now})
	if err != nil {
		return nil, err
	}
	// TODO: write manifest & config
	return store.NewImage(id, names, now, layerId, cfg), nil
}

func (s *Store) Image(id string) (r *store.Image, err error) {
	img, err := s.store.Image(id)
	if err == nil {
		r = store.NewImage(img.ID, img.Names, img.Created)
	}
	return
}

func (s *Store) ImageByName(name string) (r *store.Image, err error) {
	if imgRef, e := alltransports.ParseImageName(name); e == nil {
		name = store.ToName(imgRef)
	}
	var id string
	if id, err = s.store.Lookup(name); err == nil {
		r, err = s.Image(id)
	}
	return r, errors.Wrapf(err, "store: image %q lookup", name)
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

func (s *Store) Container(id string) (c *store.Container, err error) {
	sc, err := s.store.Container(id)
	if err != nil {
		return
	}
	return store.NewContainer(sc.ID), nil
}

func (s *Store) CreateContainer(id string, layerId string) (r *store.Container, err error) {
	c, err := s.store.CreateContainer(id, []string{}, "", layerId, "", nil)
	if err != nil {
		return
	}
	return store.NewContainer(c.ID), nil
}

func (s *Store) Mount(containerId string) (m *store.ContainerMount, err error) {
	rootfs, err := s.store.Mount(containerId, "")
	if err != nil {
		return
	}
	bundleDir := filepath.Join(rootfs, "..")
	return store.NewContainerMount(containerId, bundleDir, "rootfs"), nil
}

func (s *Store) Unmount(containerId string) error {
	return s.store.Unmount(containerId)
}

func (s *Store) Diff(containerId string) (io.ReadCloser, error) {
	//gzip := archive.Gzip
	return s.store.Diff("", containerId, &storage.DiffOptions{ /*Compression: &gzip*/ })
}

func (s *Store) PutLayer(parent string, diff archive.Reader) (*store.Layer, error) {
	return s.store.PutLayer("", parent, []string{}, "", false, diff)
}

func (s *Store) ImageConfig(imageId string) (r *ispecs.Image, err error) {
	m, err := s.imageManifest(imageId)
	if err != nil {
		return
	}
	r = &ispecs.Image{}
	b, err := s.store.ImageBigData(imageId, m.Config.Digest.String())
	if err != nil {
		return
	}
	if err = json.Unmarshal(b, r); err != nil {
		err = errors.Wrapf(err, "Cannot read image %q spec", imageId)
	}
	return
}

func (s *Store) imageManifest(imageId string) (m *ispecs.Manifest, err error) {
	mj, err := s.store.ImageBigData(imageId, "manifest")
	if err != nil {
		return
	}
	m = &ispecs.Manifest{}
	if err = json.Unmarshal(mj, m); err != nil {
		err = errors.Wrapf(err, "read image %q manifest", imageId)
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
