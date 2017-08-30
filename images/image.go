package images

import (
	"fmt"
	"github.com/mgoltzsche/cntnr/log"
	//"github.com/opencontainers/image-tools/image"
	specs "github.com/opencontainers/image-spec/specs-go/v1"
	"os"
	"path/filepath"
	"time"
	//"github.com/openSUSE/umoci/oci/layer"
	digest "github.com/opencontainers/go-digest"
)

type Image struct {
	name     string
	digest   digest.Digest
	created  time.Time
	dir      string
	manifest *specs.Manifest `json:"manifest"`
	config   *specs.Image    `json:"config"`
}

func (img *Image) Unpack(dest string, debug log.Logger) (err error) {
	if dest == "" {
		return fmt.Errorf("No image extraction destination provided")
	}

	if _, err = os.Stat(dest); !os.IsNotExist(err) {
		return fmt.Errorf("Cannot unpack image since destination already exists: %s", dest)
	}
	if err = os.MkdirAll(dest, 0770); err != nil {
		return fmt.Errorf("Cannot unpack image: %s", err)
	}

	defer func() {
		if err != nil {
			os.RemoveAll(dest)
		}
	}()

	manifest, err := img.Manifest()
	if err != nil {
		return err
	}
	for _, l := range manifest.Layers {
		if l.MediaType != specs.MediaTypeImageLayerGzip {
			return fmt.Errorf("Unsupported layer media type %q", l.MediaType)
		}

		layerFile := filepath.Join(img.dir, "blobs", l.Digest.Algorithm().String(), l.Digest.Hex())
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

func (img *Image) Name() string {
	return img.name
}

func (img *Image) Digest() digest.Digest {
	return img.digest
}

func (img *Image) Created() time.Time {
	return img.created
}

func (img *Image) Size() (size uint64, err error) {
	m, err := img.Manifest()
	if err != nil {
		return
	}
	s, err := os.Stat(blobFile(img.dir, img.Digest()))
	if err != nil {
		return
	}
	size = uint64(s.Size() + m.Config.Size)
	if m.Layers != nil {
		for _, d := range m.Layers {
			if d.Size > 0 {
				size += uint64(d.Size)
			}
		}
	}
	return
}

func (img *Image) Manifest() (*specs.Manifest, error) {
	if img.manifest == nil {
		manifestFile := blobFile(img.dir, img.Digest())
		manifest := &specs.Manifest{}
		if err := unmarshalJSON(manifestFile, &manifest); err != nil {
			return nil, fmt.Errorf("Cannot read image manifest %s: %s", img.Digest, err)
		}
		img.manifest = manifest
	}
	return img.manifest, nil
}

func (img *Image) Config() (*specs.Image, error) {
	if img.config == nil {
		m, err := img.Manifest()
		if err != nil {
			return nil, err
		}
		configFile := blobFile(img.dir, m.Config.Digest)
		config := &specs.Image{}
		if err = unmarshalJSON(configFile, config); err != nil {
			return nil, fmt.Errorf("Cannot read image config: %s", err)
		}
		img.config = config
	}
	return img.config, nil
}

func blobFile(dir string, digest digest.Digest) string {
	return filepath.Join(dir, "blobs", string(digest.Algorithm()), digest.Hex())
}
