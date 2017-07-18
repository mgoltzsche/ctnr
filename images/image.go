package images

import (
	"fmt"
	specs "github.com/opencontainers/image-spec/specs-go/v1"
)

func (img *Image) Unpack(dest string) error {
	// TODO: unpack image fs
	// Create directory
	/*if _, err := os.Stat(dest); !os.IsNotExist(err) {
		return fmt.Errorf("Cannot unpack image since destination already exists: %s", dest)
	}
	err := os.MkdirAll(dest, 0770)
	if err != nil {
		return fmt.Errorf("Cannot create container: %v", err)
	}*/

	annotations := map[string]string{}
	for _, l := range img.Manifest.Layers {
		if l.MediaType != specs.MediaTypeImageLayer {
			return fmt.Errorf("Unsupported layer media type %q", l.MediaType)
		}

		for k, v := range l.Annotations {
			annotations[k] = v
		}

		fmt.Println(l.Digest)
	}
	return nil
}
