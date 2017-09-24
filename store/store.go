package store

import (
	"encoding/base32"
	"strings"
	"time"

	"github.com/containers/image/types"
	"github.com/mgoltzsche/cntnr/generate"
	digest "github.com/opencontainers/go-digest"
	ispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/satori/go.uuid"
)

// TODO: Make sure store is closed before running any container to free shared lock to allow other container to be prepared
// TODO: base Commit method in BlobStore (so that mtree can move to blobstore), UnpackImage method in ImageStore

// Minimal Store interface.
// containers/storage interface is not used to ease the OCI store implementation
// which is required by unprivileged users (https://github.com/containers/storage/issues/96)
type Store interface {
	ImportImage(name string) (Image, error)
	Image(id digest.Digest) (Image, error)
	ImageByName(name string) (Image, error)
	Images() ([]Image, error)
	ImageManifest(d digest.Digest) (ispecs.Manifest, error)
	ImageConfig(d digest.Digest) (ispecs.Image, error)
	PutImageManifest(m ispecs.Manifest) (ispecs.Descriptor, error)
	PutImageConfig(m ispecs.Image) (ispecs.Descriptor, error)
	CreateImage(name, ref string, manifestDigest digest.Digest) (Image, error)
	DeleteImage(name, ref string) error
	ImageGC() error
	CreateContainer(id string, manifestDigest *digest.Digest) (*ContainerBuilder, error)
	Container(id string) (Container, error)
	Containers() ([]Container, error)
	DeleteContainer(id string) error
	Commit(containerId, author, comment string) (CommitResult, error)
	Close() error
}

type CommitResult struct {
	Manifest   ispecs.Manifest
	Config     ispecs.Image
	Descriptor ispecs.Descriptor
}

type Image struct {
	ID       digest.Digest
	Name     string
	Ref      string
	Manifest ispecs.Manifest
	Size     uint64
	Created  time.Time
}

func NewImage(id digest.Digest, name, ref string, created time.Time, manifest ispecs.Manifest) Image {
	var size uint64
	for _, l := range manifest.Layers {
		if l.Size > 0 {
			size += uint64(l.Size)
		}
	}
	return Image{id, name, ref, manifest, size, created}
}

type Container struct {
	ID      string
	Dir     string
	Image   *digest.Digest
	Created time.Time
}

func NewContainer(id, dir string, image *digest.Digest, created time.Time) Container {
	return Container{id, dir, image, created}
}

type ContainerBuilder struct {
	ID                  string
	Dir                 string
	ImageManifestDigest *digest.Digest
	*generate.SpecBuilder
	build func(*ContainerBuilder) (Container, error)
}

func NewContainerBuilder(id, dir string, imageManifestDigest *digest.Digest, spec *generate.SpecBuilder, build func(*ContainerBuilder) (Container, error)) *ContainerBuilder {
	return &ContainerBuilder{id, dir, imageManifestDigest, spec, build}
}

func (b *ContainerBuilder) Build() (Container, error) {
	return b.build(b)
}

// Generate or move into utils package since it also occurs in run
func GenerateId() string {
	return strings.TrimRight(base32.StdEncoding.EncodeToString(uuid.NewV4().Bytes()), "=")
}

func NameAndRef(imgRef types.ImageReference) (name string, tag string) {
	name = strings.TrimLeft(imgRef.StringWithinTransport(), "/")
	dckrRef := imgRef.DockerReference()
	if dckrRef != nil {
		name = dckrRef.String()
	}
	li := strings.LastIndex(name, ":")
	if li > 0 && li+1 < len(name) {
		tag = name[li+1:]
		name = name[:li]
	} else {
		tag = "latest"
	}
	return
}
