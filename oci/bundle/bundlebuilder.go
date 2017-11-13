package bundle

import (
	"encoding/base32"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mgoltzsche/cntnr/generate"
	gen "github.com/opencontainers/runtime-tools/generate"
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
	spec.AddAnnotation(ANNOTATION_BUNDLE_IMAGE, image.ID())
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

func (b *BundleBuilder) Build(dir string) (r Bundle, err error) {
	r.id = b.id
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

func generateId() string {
	return strings.ToLower(strings.TrimRight(base32.StdEncoding.EncodeToString(uuid.NewV4().Bytes()), "="))
}
