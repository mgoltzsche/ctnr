package images

import (
	"fmt"
	specs "github.com/opencontainers/image-spec/specs-go/v1"
	//"github.com/opencontainers/image-tools/image"
	//"github.com/openSUSE/umoci/oci/layer"
	"github.com/mgoltzsche/cntnr/log"
	"os"
	"path/filepath"
)

func (img *Image) Unpack(dest string, debug log.Logger) error {
	if dest == "" {
		return fmt.Errorf("No archive extraction destination provided")
	}

	// TODO: unpack image fs
	// Create directory
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		return fmt.Errorf("Cannot unpack image since destination already exists: %s", dest)
	}
	err := os.MkdirAll(dest, 0770)
	if err != nil {
		return fmt.Errorf("Cannot create container: %v", err)
	}

	annotations := map[string]string{}
	for _, l := range img.Manifest.Layers {
		if l.MediaType != specs.MediaTypeImageLayerGzip {
			return fmt.Errorf("Unsupported layer media type %q", l.MediaType)
		}

		for k, v := range l.Annotations {
			annotations[k] = v
		}

		layerFile := filepath.Join(img.Directory, "blobs", l.Digest.Algorithm().String(), l.Digest.Hex())
		debug.Printf("Extracting layer %s", l.Digest.String())

		if err := unpackLayer(layerFile, dest); err != nil {
			return err
		}
	}

	/*err = image.UnpackLayout(img.Directory, containerDir, "latest")
	if err != nil {
		return nil, fmt.Errorf("Unpacking OCI layout of image %q (%s) failed: %v", service.Image, img.Directory, err)
	}*/

	return nil
}
