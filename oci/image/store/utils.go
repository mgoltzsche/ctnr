package store

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/containers/image/signature"
	"github.com/containers/image/transports/alltransports"
	"github.com/containers/image/types"
	"github.com/mgoltzsche/cntnr/pkg/lock"
	ispecs "github.com/opencontainers/image-spec/specs-go/v1"
)

func imageIndex(dir string, r *ispecs.Index) error {
	idxFile := filepath.Join(dir, "index.json")
	b, err := ioutil.ReadFile(idxFile)
	if err != nil {
		return fmt.Errorf("read image index: %s", err)
	}
	if err = json.Unmarshal(b, r); err != nil {
		return fmt.Errorf("unmarshal image index %s: %s", idxFile, err)
	}
	return nil
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

func parseImageName(nameAndRef string) (name, ref string) {
	if li := strings.LastIndex(nameAndRef, ":"); li > 0 && li+1 < len(nameAndRef) {
		ref = nameAndRef[li+1:]
		name = nameAndRef[:li]
	} else {
		ref = "latest"
	}
	return
}

func createTrustPolicyContext() (*signature.PolicyContext, error) {
	policyFile := ""
	var policy *signature.Policy // This could be cached across calls, if we had an application context.
	var err error
	//if insecurePolicy {
	//	policy = &signature.Policy{Default: []signature.PolicyRequirement{signature.NewPRInsecureAcceptAnything()}}
	if policyFile == "" {
		policy, err = signature.DefaultPolicy(nil)
	} else {
		policy, err = signature.NewPolicyFromFile(policyFile)
	}
	if err != nil {
		return nil, err
	}
	return signature.NewPolicyContext(policy)
}

// TODO: Move into imagerepo
func findManifestDigest(idx *ispecs.Index, ref string) (d ispecs.Descriptor, err error) {
	refFound := false
	for _, descriptor := range idx.Manifests {
		if descriptor.Annotations[ispecs.AnnotationRefName] == ref {
			refFound = true
			if descriptor.Platform.Architecture == runtime.GOARCH && descriptor.Platform.OS == runtime.GOOS {
				if descriptor.MediaType != ispecs.MediaTypeImageManifest {
					err = fmt.Errorf("unsupported manifest media type %q", descriptor.MediaType)
				}
				return descriptor, err
			}
		}
	}
	if refFound {
		err = fmt.Errorf("no image manifest for architecture %s and OS %s found in image index!", runtime.GOARCH, runtime.GOOS)
	} else {
		err = fmt.Errorf("no image manifest for ref %q found in image index!", ref)
	}
	return
}

func unlock(lock lock.Locker, err *error) {
	if e := lock.Unlock(); e != nil {
		if *err == nil {
			*err = e
		} else {
			fmt.Fprint(os.Stderr, "Error: %s", e)
		}
	}
}
