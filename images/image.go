package images

import (
	"fmt"
	"github.com/mgoltzsche/cntnr/log"
	//"github.com/opencontainers/image-tools/image"
	specs "github.com/opencontainers/image-spec/specs-go/v1"
	"os"
	"path/filepath"
	//"github.com/openSUSE/umoci/oci/layer"
)

func (img *Image) Unpack(dest string, debug log.Logger) (err error) {
	if dest == "" {
		return fmt.Errorf("No image extraction destination provided")
	}

	if _, err = os.Stat(dest); !os.IsNotExist(err) {
		return fmt.Errorf("Cannot unpack image since destination already exists: %s", dest)
	}
	err = os.MkdirAll(dest, 0770)
	if err != nil {
		return fmt.Errorf("Cannot unpack image: %s", err)
	}

	defer func() {
		if err != nil {
			os.RemoveAll(dest)
		}
	}()

	for _, l := range img.Manifest.Layers {
		if l.MediaType != specs.MediaTypeImageLayerGzip {
			return fmt.Errorf("Unsupported layer media type %q", l.MediaType)
		}

		layerFile := filepath.Join(img.Directory, "blobs", l.Digest.Algorithm().String(), l.Digest.Hex())
		debug.Printf("Extracting layer %s", l.Digest.String())

		if err := unpackLayer(layerFile, dest); err != nil {
			return err
		}
	}

	/*err = image.UnpackLayout(img.Directory, dest, os.Platform)
	if err != nil {
		err = fmt.Errorf("Unpacking OCI layout of image %q (%s) failed: %s", service.Image, img.Directory, err)
	}*/
	return
}
