package store

import (
	"encoding/json"
	"io/ioutil"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/containers/image/transports/alltransports"
	"github.com/containers/image/types"
	exterrors "github.com/mgoltzsche/cntnr/pkg/errors"
	"github.com/mgoltzsche/cntnr/pkg/lock"
	ispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
)

func imageIndex(dir string, r *ispecs.Index) (err error) {
	idxFile := filepath.Join(dir, "index.json")
	b, err := ioutil.ReadFile(idxFile)
	if err != nil {
		return errors.New("read image index: " + err.Error())
	}
	if err = json.Unmarshal(b, r); err != nil {
		err = errors.New("unmarshal image index " + idxFile + ": " + err.Error())
	}
	return
}

func normalizeImageName(nameAndTag string) (name, ref string) {
	imgRef, err := alltransports.ParseImageName(nameAndTag)
	if err != nil {
		return parseImageName(nameAndTag)
	}
	return nameAndRef(imgRef)
}

func nameAndRef(imgRef types.ImageReference) (string, string) {
	name := strings.TrimLeft(imgRef.StringWithinTransport(), "/")
	dckrRef := imgRef.DockerReference()
	if dckrRef != nil {
		name = dckrRef.String()
	}
	return parseImageName(name)
}

func parseImageName(nameAndRef string) (repo, ref string) {
	if li := strings.LastIndex(nameAndRef, ":"); li > 0 && li+1 < len(nameAndRef) {
		repo = nameAndRef[:li]
		ref = nameAndRef[li+1:]
	} else {
		repo = nameAndRef
		ref = "latest"
	}
	return
}

// TODO: Move into imagerepo
func findManifestDigest(idx *ispecs.Index, ref string) (d ispecs.Descriptor, err error) {
	refFound := false
	for _, descriptor := range idx.Manifests {
		if descriptor.Annotations[ispecs.AnnotationRefName] == ref {
			refFound = true
			if descriptor.Platform.Architecture == runtime.GOARCH && descriptor.Platform.OS == runtime.GOOS {
				if descriptor.MediaType != ispecs.MediaTypeImageManifest {
					err = errors.Errorf("unsupported manifest media type %q", descriptor.MediaType)
				}
				return descriptor, err
			}
		}
	}
	if refFound {
		err = errors.Errorf("no image manifest for architecture %s and OS %s found in image index!", runtime.GOARCH, runtime.GOOS)
	} else {
		err = errors.Errorf("no image manifest for ref %q found in image index!", ref)
	}
	return
}

func unlock(lock lock.Locker, err *error) {
	*err = exterrors.Append(*err, lock.Unlock())
}
