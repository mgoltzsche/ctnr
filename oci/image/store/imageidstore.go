package store

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mgoltzsche/cntnr/oci/image"
	"github.com/mgoltzsche/cntnr/pkg/atomic"
	digest "github.com/opencontainers/go-digest"
)

var _ image.ImageTagStore = &ImageTagStore{}

type ImageIdStore struct {
	dir string
}

type ImageID struct {
	ID             digest.Digest
	ManifestDigest digest.Digest
	LastUsed       time.Time
}

func (s ImageID) String() string {
	return fmt.Sprintf("%s -> %s", s.ID, s.ManifestDigest)
}

func NewImageIdStore(dir string) (r ImageIdStore) {
	r.dir = dir
	return
}

func (s *ImageIdStore) Add(imageID, manifestDigest digest.Digest) (err error) {
	if err = imageID.Validate(); err != nil {
		return fmt.Errorf("add image ID %s -> %s: invalid image ID: %s", imageID, manifestDigest, err)
	}
	if err = manifestDigest.Validate(); err != nil {
		return fmt.Errorf("add image ID %s -> %s: invalid manifest Digest: %s", imageID, manifestDigest, err)
	}
	if err = os.MkdirAll(filepath.Join(s.dir), 0775); err != nil {
		return fmt.Errorf("add image ID %s: %s", imageID, err)
	}
	file := s.idFile(imageID)
	if _, err = atomic.WriteFile(file, bytes.NewBufferString(manifestDigest.String())); err != nil {
		err = fmt.Errorf("add image ID %s: %s", imageID, err)
	}
	return
}

func (s *ImageIdStore) Del(imageID digest.Digest) (err error) {
	if err = os.Remove(s.idFile(imageID)); err != nil {
		err = fmt.Errorf("delete image ID %s: %s", imageID, err)
	}
	return
}

func (s *ImageIdStore) ImageID(imageID digest.Digest) (r ImageID, err error) {
	r.ID = imageID
	file := s.idFile(imageID)
	f, err := os.Stat(file)
	if err != nil {
		return r, fmt.Errorf("image ID %s: %s", imageID, err)
	}
	r.LastUsed = f.ModTime()
	if r.ManifestDigest, err = readImageIDFile(file); err != nil {
		err = fmt.Errorf("image ID %s: %s", imageID, err)
	}
	return
}

func (s *ImageIdStore) ImageIDs() (r []ImageID, err error) {
	fl, err := ioutil.ReadDir(s.dir)
	r = make([]ImageID, 0, len(fl))
	if err != nil && !os.IsNotExist(err) {
		return r, fmt.Errorf("image IDs: %s", err)
	}
	if len(fl) > 0 {
		for _, f := range fl {
			if !f.IsDir() {
				imageID, e := decodeImageIdFileName(f.Name())
				if e == nil {
					img, e := s.ImageID(imageID)
					if e == nil {
						r = append(r, img)
					} else if err == nil {
						err = e
					}
				}
			}
		}
	}
	return
}

func (s *ImageIdStore) MarkUsed(id digest.Digest) (err error) {
	now := time.Now()
	if err = os.Chtimes(s.idFile(id), now, now); err != nil {
		err = fmt.Errorf("mark used image ID: %s", err)
	}
	return
}

func (s *ImageIdStore) idFile(imageId digest.Digest) string {
	return filepath.Join(s.dir, imageId.Algorithm().String()+"-"+imageId.Hex())
}

func decodeImageIdFileName(fileName string) (r digest.Digest, err error) {
	idStr := strings.Replace(fileName, "-", ":", 1)
	if r, err = digest.Parse(idStr); err != nil {
		err = fmt.Errorf("parse image ID file name %q: %s", idStr, err)
	}
	if err = r.Validate(); err != nil {
		err = fmt.Errorf("invalid image ID file name %q: %s", idStr, err)
	}
	return
}

func readImageIDFile(file string) (imageID digest.Digest, err error) {
	f, err := os.Open(file)
	if err != nil {
		return
	}
	defer f.Close()
	b, err := ioutil.ReadAll(f)
	if err != nil {
		return
	}
	imageID, err = digest.Parse(string(b))
	return
}
