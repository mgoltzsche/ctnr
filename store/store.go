package store

import (
	"encoding/base32"
	"strings"

	imgspecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/opencontainers/runtime-tools/generate"

	"time"

	"github.com/containers/image/types"
	"github.com/satori/go.uuid"
)

type Store interface {
	ImportImage(name string) (*Image, error)
	Image(id string) (*Image, error)
	ImageByName(name string) (*Image, error)
	ImageConfig(id string) (*imgspecs.Image, error)
	Images() ([]*Image, error)
	DeleteImage(id string) error
	ImageGC() error
	CreateContainer(id string, spec *generate.Generator, image string) (*Container, error)
	/*Containers() (*Container)
	CreateContainer(id, layerId string) (string, error)
	DeleteContainer(id) (string, error)*/
	Close() error
}

type Image struct {
	id      string
	names   []string
	created time.Time
}

func NewImage(id string, names []string, created time.Time) *Image {
	return &Image{id, names, created}
}

func (img *Image) ID() string {
	return img.id
}

func (img *Image) Names() []string {
	return img.names
}

func (img *Image) Created() time.Time {
	return img.created
}

type Container struct {
	id  string
	dir string
}

func NewContainer(id, dir string) *Container {
	return &Container{id, dir}
}

func (c *Container) ID() string {
	return c.id
}

func (c *Container) Dir() string {
	return c.dir
}

// Generate or move into utils package since it also occurs in run
func GenerateId() string {
	return strings.TrimRight(base32.StdEncoding.EncodeToString(uuid.NewV4().Bytes()), "=")
}

func ToName(imgRef types.ImageReference) string {
	name := strings.TrimLeft(imgRef.StringWithinTransport(), "/")
	dckrRef := imgRef.DockerReference()
	if dckrRef != nil {
		name = dckrRef.String()
	}
	return name
}
