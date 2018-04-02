package bundle

import (
	"encoding/base32"
	"strings"

	"github.com/mgoltzsche/cntnr/pkg/generate"
	"github.com/pkg/errors"
	"github.com/satori/go.uuid"
)

type BundleBuilder struct {
	id string
	*generate.SpecBuilder
	image BundleImage
}

func Builder(id string) *BundleBuilder {
	spec := generate.NewSpecBuilder()
	spec.AddAnnotation(ANNOTATION_BUNDLE_ID, id)
	spec.SetRootPath("rootfs")
	return FromSpec(&spec)
}

func BuilderFromImage(id string, image BundleImage) (b *BundleBuilder, err error) {
	spec := generate.NewSpecBuilder()
	spec.SetRootPath("rootfs")
	conf, err := image.Config()
	if err == nil {
		spec.ApplyImage(conf)
		spec.AddAnnotation(ANNOTATION_BUNDLE_ID, id)
		b = FromSpec(&spec)
		b.image = image
	}
	return b, errors.Wrap(err, "bundle build from image")
}

func FromSpec(spec *generate.SpecBuilder) *BundleBuilder {
	id := ""
	if s := spec.Generator.Spec(); s != nil && s.Annotations != nil {
		id = s.Annotations[ANNOTATION_BUNDLE_ID]
	}
	if id == "" {
		id = generateId()
	}
	b := &BundleBuilder{"", spec, nil}
	b.SetID(id)
	return b
}

func (b *BundleBuilder) SetID(id string) {
	if id == "" {
		panic("no bundle id provided")
	}
	b.id = id
	b.SetHostname(id)
	b.AddAnnotation(ANNOTATION_BUNDLE_ID, id)
}

func (b *BundleBuilder) GetID() string {
	return b.id
}

func (b *BundleBuilder) Build(dir string, update bool) (*LockedBundle, error) {
	// Create bundle directory
	bundle, err := CreateLockedBundle(dir, b, b.image, update)
	return bundle, errors.Wrap(err, "build bundle")
}

func generateId() string {
	return strings.ToLower(strings.TrimRight(base32.StdEncoding.EncodeToString(uuid.NewV4().Bytes()), "="))
}
