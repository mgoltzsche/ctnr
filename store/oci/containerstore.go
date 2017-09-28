package oci

import (
	"fmt"
	//"io"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/mgoltzsche/cntnr/generate"
	"github.com/mgoltzsche/cntnr/log"
	"github.com/mgoltzsche/cntnr/store"
	//"github.com/openSUSE/umoci/oci/layer"
	digest "github.com/opencontainers/go-digest"
	gen "github.com/opencontainers/runtime-tools/generate"
)

var _ store.Store = &ContainerStore{}

type ContainerStore struct {
	*ImageStore
	containerDir string
	debug        log.Logger
}

func NewContainerStore(dir string, images *ImageStore, debugLog log.Logger) (s ContainerStore, err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("init container store: %s", err)
		}
	}()
	s.ImageStore = images
	s.debug = debugLog
	s.containerDir, err = filepath.Abs(dir)
	if err != nil {
		return
	}
	if err = os.MkdirAll(s.containerDir, 0755); err != nil {
		return
	}
	return
}

func (s *ContainerStore) Containers() ([]store.Container, error) {
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
				s.debug.Printf("containers: %s", err)
			}
		}
	}
	return l, nil
}

// TODO: lock container
func (s *ContainerStore) Container(id string) (c store.Container, err error) {
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

func (s *ContainerStore) DeleteContainer(id string) (err error) {
	if err = os.RemoveAll(filepath.Join(s.containerDir, id)); err != nil {
		err = fmt.Errorf("delete container: %s", err)
	}
	return
}

func (s *ContainerStore) CreateContainer(id string, imageManifestDigest *digest.Digest) (b *store.ContainerBuilder, err error) {
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

func (s *ContainerStore) buildContainer(b *store.ContainerBuilder) (c store.Container, err error) {
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
		if err = s.UnpackLayers(*b.ImageManifestDigest, rootfs); err != nil {
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

func (s *ContainerStore) Commit(containerId, author, comment string) (r store.CommitResult, err error) {
	// Load parent manifest
	manifestDigest, err := s.readParent(containerId)
	if err != nil {
		return r, fmt.Errorf("commit: %s", err)
	}

	// Commit layer
	rootfs := filepath.Join(s.containerDir, containerId, "rootfs")
	r, err = s.CommitLayer(rootfs, manifestDigest, author, comment)
	if err != nil {
		return
	}

	// Update container parent
	if err = s.writeParent(containerId, r.Descriptor.Digest); err != nil {
		return r, fmt.Errorf("commit: %s", err)
	}
	return
}

func (s *ContainerStore) writeParent(id string, manifest digest.Digest) error {
	parentFile := filepath.Join(s.containerDir, id, "parent")
	return writeFile(parentFile, []byte(manifest.String()))
}

func (s *ContainerStore) readParent(id string) (manifest *digest.Digest, err error) {
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
