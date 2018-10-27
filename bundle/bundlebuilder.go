package bundle

import (
	"path/filepath"

	"github.com/mgoltzsche/ctnr/pkg/generate"
)

type BundleBuilder struct {
	id string
	*generate.SpecBuilder
	image BundleImage
}

func Builder(id string) *BundleBuilder {
	specgen := generate.NewSpecBuilder()
	specgen.SetRootPath("rootfs")
	b := &BundleBuilder{"", &specgen, nil}
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

func (b *BundleBuilder) SetImage(image BundleImage) {
	b.ApplyImage(image.Config())
	b.image = image
}

func (b *BundleBuilder) Build(bundle *LockedBundle) (err error) {
	// Prepare rootfs
	if err = bundle.UpdateRootfs(b.image); err != nil {
		return
	}

	// Resolve user/group names
	rootfs := filepath.Join(bundle.Dir(), b.Generator.Spec().Root.Path)
	spec, err := b.Spec(rootfs)
	if err != nil {
		return
	}

	// Apply spec
	return bundle.SetSpec(spec)
}
