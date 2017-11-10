package bundle

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/mgoltzsche/cntnr/generate"
	//specs "github.com/opencontainers/runtime-spec/specs-go"
	gen "github.com/opencontainers/runtime-tools/generate"
)

type BundleBuilder struct {
	*generate.SpecBuilder
	image BundleImage `json:"-"`
}

/*type bundleMount struct {
	specs.Mount
	relSource string
}

func (m *bundleMount) setBaseDir(dir string) {
	m.Source = filepath.Join(dir, m.relSource)
}*/

func Builder() *BundleBuilder {
	spec := generate.NewSpecBuilder()
	spec.SetRootPath("rootfs")
	return FromSpec(&spec)
}

func BuilderFromImage(image BundleImage) (*BundleBuilder, error) {
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

/*func (b *BundleBuilder) AddBindMountBundleRelative(src, dest string, opts []string) {
	spec := b.Spec()
	i := len(spec.Mounts)
	b.AddBindMount(src, dest, opts)
	m := spec.Mounts[i]
	spec.Mounts[i] = bundleMount{m, src}
}*/

func (b *BundleBuilder) Build(dir string) (r Bundle, err error) {
	r.dir = dir
	r.created = time.Now()
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
		return r, fmt.Errorf("build bundle: write config.json: %s", err)
	}

	// Create volume directories
	if mounts := b.Spec().Mounts; mounts != nil {
		for _, mount := range mounts {
			if mount.Type == "bind" {
				src := mount.Source
				if !filepath.IsAbs(src) {
					src = filepath.Join(dir, src)
				}
				if _, err = os.Stat(src); os.IsNotExist(err) {
					if err = os.MkdirAll(src, 0755); err != nil {
						return
					}
				}
			}
		}
	}
	return
}
