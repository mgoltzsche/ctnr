package store

import (
	"encoding/json"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"

	"github.com/mgoltzsche/cntnr/image"
	ispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
)

// Read-only OCI image index representation
type ImageRepo struct {
	Name  string
	index ispecs.Index
}

func NewImageRepo(name, dir string) (r *ImageRepo, err error) {
	dir = filepath.Clean(dir)
	r = &ImageRepo{Name: name}
	idxFile := filepath.Join(dir, "index.json")
	b, err := ioutil.ReadFile(idxFile)
	if err != nil {
		if os.IsNotExist(err) {
			err = image.ErrNotExist(err)
		}
		return nil, errors.Wrap(err, "read image index")
	}
	if err = json.Unmarshal(b, &r.index); err != nil {
		return nil, errors.Wrap(err, "unmarshal image index "+idxFile)
	}
	if r.index.SchemaVersion != 2 {
		return nil, errors.Errorf("unsupported image index schema version %d in %s (%q), expected version 2", r.index.SchemaVersion, idxFile, name)
	}
	return
}

// Returns all valid contained manifest descriptors and an error if a descriptor has no ref
func (s *ImageRepo) Manifests() (r []ispecs.Descriptor, err error) {
	r = []ispecs.Descriptor{}
	for _, d := range s.index.Manifests {
		ref := d.Annotations[ispecs.AnnotationRefName]
		if ref == "" {
			err = errors.Errorf("image %q index: manifest descriptor has no ref", s.Name)
		} else {
			r = append(r, d)
		}
	}
	return
}

// Returns the manifest descriptor matching the given ref or an error.
func (s *ImageRepo) Manifest(ref string) (d ispecs.Descriptor, err error) {
	foundPlatform := ""
	for _, descriptor := range s.index.Manifests {
		if descriptor.Annotations[ispecs.AnnotationRefName] == ref {
			if descriptor.Platform.Architecture == runtime.GOARCH && descriptor.Platform.OS == runtime.GOOS {
				if descriptor.MediaType != ispecs.MediaTypeImageManifest {
					err = errors.Errorf("unsupported manifest media type %q", descriptor.MediaType)
				}
				return descriptor, err
			} else {
				foundPlatform = descriptor.Platform.OS + "-" + descriptor.Platform.Architecture
			}
		}
	}
	if foundPlatform != "" {
		err = errors.Errorf("repo %s: image ref %q not found for supported platform %s-%s but %q", s.Name, ref, runtime.GOOS, runtime.GOARCH, foundPlatform)
	} else {
		err = errors.Errorf("repo %s: image ref %q not found", s.Name, ref)
	}
	return d, image.ErrNotExist(err)
}
