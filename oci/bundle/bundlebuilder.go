package bundle

import (
	"encoding/base32"
	"fmt"
	"strings"

	"github.com/mgoltzsche/cntnr/generate"
	"github.com/opencontainers/runtime-tools/generate/seccomp"
	"github.com/satori/go.uuid"
)

const ANNOTATION_BUNDLE_ID = "com.github.mgoltzsche.cntnr.bundle.id"

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

func BuilderFromImage(id string, image BundleImage) (*BundleBuilder, error) {
	spec := generate.NewSpecBuilder()
	spec.SetRootPath("rootfs")
	conf, err := image.Config()
	if err != nil {
		return nil, fmt.Errorf("bundle build from image: %s", err)
	}
	spec.ApplyImage(conf)
	spec.AddAnnotation(ANNOTATION_BUNDLE_ID, id)
	r := FromSpec(&spec)
	r.image = image
	return r, nil
}

func FromSpec(spec *generate.SpecBuilder) *BundleBuilder {
	id := ""
	if s := spec.Spec(); s != nil && s.Annotations != nil {
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

func (b *BundleBuilder) Build(dir string, update bool) (bundle *LockedBundle, err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("build bundle: %s", err)
		}
	}()

	spec := b.Spec()
	spec.Linux.Seccomp = seccomp.DefaultProfile(spec)

	// Create bundle directory
	if bundle, err = CreateLockedBundle(dir, &b.Generator, b.image, update); err != nil {
		return
	}

	return
}

func generateId() string {
	return strings.ToLower(strings.TrimRight(base32.StdEncoding.EncodeToString(uuid.NewV4().Bytes()), "="))
}
