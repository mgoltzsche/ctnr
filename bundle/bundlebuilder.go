package bundle

import (
	"os"
	"path/filepath"

	"github.com/cyphar/filepath-securejoin"
	"github.com/mgoltzsche/ctnr/pkg/generate"
	"github.com/openSUSE/umoci/pkg/fseval"
	"github.com/pkg/errors"
)

type BundleBuilder struct {
	id string
	*generate.SpecBuilder
	image        BundleImage
	managedFiles map[string]bool
}

func Builder(id string) *BundleBuilder {
	specgen := generate.NewSpecBuilder()
	specgen.SetRootPath("rootfs")
	b := &BundleBuilder{"", &specgen, nil, map[string]bool{}}
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

func (b *BundleBuilder) SetImage(image BundleImage) {
	b.ApplyImage(image.Config())
	b.image = image
}

// Overlays the provided file path with a bind mounted read-only copy.
// The file's content is supposed to be managed by an OCI hook.
func (b *BundleBuilder) AddBindMountConfig(path string) {
	path = filepath.Clean(path)
	opts := []string{"bind", "mode=0444", "nosuid", "noexec", "nodev", "ro"}
	b.managedFiles[path] = true
	b.AddBindMount(filepath.Join("mount", path), path, opts)
}

func (b *BundleBuilder) Build(bundle *LockedBundle) (err error) {
	// Prepare rootfs
	if err = bundle.UpdateRootfs(b.image); err != nil {
		return errors.Wrap(err, "build bundle")
	}

	// Generate managed config files
	for path := range b.managedFiles {
		if err = b.touchManagedFile(bundle.Dir(), path); err != nil {
			return errors.Wrap(err, "build bundle")
		}
	}

	// Resolve user/group names
	rootfs := filepath.Join(bundle.Dir(), b.Generator.Spec().Root.Path)
	spec, err := b.Spec(rootfs)
	if err != nil {
		return errors.Wrap(err, "build bundle")
	}

	// Apply spec
	return errors.Wrap(bundle.SetSpec(spec), "build bundle")
}

func (b *BundleBuilder) touchManagedFile(bundleDir, path string) (err error) {
	file, err := securejoin.SecureJoinVFS(filepath.Join(bundleDir, "mount"), path, fseval.RootlessFsEval)
	if err != nil {
		return
	}
	if err = os.MkdirAll(filepath.Dir(file), 0755); err != nil {
		return
	}
	f, err := os.OpenFile(file, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return
	}
	return f.Close()
}
