package bundle

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mgoltzsche/cntnr/generate"
	gen "github.com/opencontainers/runtime-tools/generate"
)

type BundleBuilder struct {
	*generate.SpecBuilder
	image Image
}

func Builder() *BundleBuilder {
	spec := generate.NewSpecBuilder()
	spec.SetRootPath("rootfs")
	return FromSpec(&spec)
}

func BuilderFromImage(image Image) (*BundleBuilder, error) {
	spec := generate.NewSpecBuilder()
	spec.SetRootPath("rootfs")
	conf, err := image.Config()
	if err != nil {
		return nil, fmt.Errorf("bundle build from image: %s", err)
	}
	spec.ApplyImage(conf)
	spec.AddAnnotation(ANNOTATION_BUNDLE_IMAGE, image.ID())
	r := FromSpec(&spec)
	r.image = image
	return r, nil
}

func FromSpec(spec *generate.SpecBuilder) *BundleBuilder {
	return &BundleBuilder{spec, nil}
}

func (b *BundleBuilder) Build(dir string) (r Bundle, err error) {
	r.dir = dir
	rootfs := filepath.Join(dir, b.Spec().Root.Path)

	// Create bundle directory
	if err = os.Mkdir(dir, 0770); err != nil {
		err = fmt.Errorf("build bundle: %s", err)
		return
	}
	defer func() {
		if err != nil {
			os.RemoveAll(dir)
		}
	}()

	// Prepare rootfs
	if b.image != nil {
		if err = b.image.Unpack(rootfs); err != nil {
			return r, fmt.Errorf("build bundle: %s", err)
		}
	} else if err = os.Mkdir(rootfs, 0755); err != nil {
		return r, fmt.Errorf("build bundle: %s", err)
	}

	// Write runtime config
	confFile := filepath.Join(r.dir, "config.json")
	err = b.SaveToFile(confFile, gen.ExportOptions{Seccomp: false})
	if err != nil {
		err = fmt.Errorf("write bundle config.json: %s", err)
	}
	return
}
