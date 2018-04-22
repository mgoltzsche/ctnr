package store

import (
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/mgoltzsche/cntnr/image"
	"github.com/pkg/errors"
)

type ImageFSROCache struct {
	dir string
}

func NewImageFSROCache(dir string) *ImageFSROCache {
	return &ImageFSROCache{dir}
}

func (s *ImageFSROCache) GetRootfs(img *image.Image) (dir string, err error) {
	dir = filepath.Join(s.dir, img.ID().Algorithm().String(), img.ID().Hex())
	if _, err = os.Stat(dir); err == nil {
		return
	}
	var tmpDir string
	if os.IsNotExist(err) {
		parentDir := filepath.Dir(dir)
		if err = os.MkdirAll(parentDir, 0755); err == nil {
			tmpDir, err = ioutil.TempDir(parentDir, ".tmp-"+img.ID().Hex()+"-")
		}
	}
	if err == nil {
		defer os.RemoveAll(tmpDir)
		if err = img.Unpack(tmpDir); err != nil {
			return
		}
		err = os.Rename(tmpDir, dir)
	}
	if err != nil {
		err = errors.New("imagefs cache: " + err.Error())
	}
	return
}
