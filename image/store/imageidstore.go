package store

import (
	"bytes"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mgoltzsche/cntnr/image"
	"github.com/mgoltzsche/cntnr/pkg/atomic"
	exterrors "github.com/mgoltzsche/cntnr/pkg/errors"
	digest "github.com/opencontainers/go-digest"
	"github.com/pkg/errors"
)

type ImageIdStore struct {
	dir string
}

type ImageID struct {
	ID             digest.Digest
	ManifestDigest digest.Digest
	LastUsed       time.Time
}

func (s ImageID) String() string {
	return s.ID.String() + " -> " + s.ManifestDigest.String()
}

func NewImageIdStore(dir string) (r ImageIdStore) {
	r.dir = dir
	return
}

func (s *ImageIdStore) Add(imageID, manifestDigest digest.Digest) (err error) {
	defer func() {
		err = errors.Wrapf(err, "add image ID %s -> %s", imageID, manifestDigest)
	}()
	file, err := s.idFile(imageID)
	if err != nil {
		return
	}
	if err = manifestDigest.Validate(); err != nil {
		return errors.New("invalid manifest digest: " + manifestDigest.String())
	}
	if err = os.MkdirAll(filepath.Join(s.dir), 0775); err != nil {
		return errors.New(err.Error())
	}
	_, err = atomic.WriteFile(file, bytes.NewBufferString(manifestDigest.String()))
	return
}

func (s *ImageIdStore) Del(imageID digest.Digest) (err error) {
	f, err := s.idFile(imageID)
	if err == nil {
		if err = os.Remove(f); err != nil {
			if os.IsNotExist(err) {
				err = image.ErrorImageIdNotExist("image %s does not exist", imageID)
			} else {
				err = errors.Errorf(err.Error())
			}
		}
	}
	errors.WithMessage(err, "delete image ID")
	return
}

func (s *ImageIdStore) ImageID(imageID digest.Digest) (r ImageID, err error) {
	r.ID = imageID
	file, err := s.idFile(imageID)
	if err != nil {
		return
	}
	f, err := os.Stat(file)
	if err == nil {
		r.LastUsed = f.ModTime()
		r.ManifestDigest, err = readImageIDFile(file)
	} else if os.IsNotExist(err) {
		err = image.ErrorImageIdNotExist("image ID %s does not exist", imageID)
	} else {
		err = errors.Errorf("lookup image ID %q: %s", imageID, err)
	}
	return
}

func (s *ImageIdStore) ImageIDs() (r []ImageID, err error) {
	fl, e := ioutil.ReadDir(s.dir)
	r = make([]ImageID, 0, len(fl))
	if e != nil && !os.IsNotExist(err) {
		return r, errors.New("image IDs: " + e.Error())
	}
	if len(fl) > 0 {
		for _, f := range fl {
			if !f.IsDir() {
				imageID, e := decodeImageIdFileName(f.Name())
				if e == nil {
					img, e := s.ImageID(imageID)
					if e == nil {
						r = append(r, img)
					} else {
						err = exterrors.Append(err, e)
					}
				}
			}
		}
	}
	return
}

func (s *ImageIdStore) MarkUsed(id digest.Digest) (err error) {
	f, err := s.idFile(id)
	if err == nil {
		now := time.Now()
		if err = os.Chtimes(f, now, now); err != nil {
			err = errors.New(err.Error())
		}
	}
	return errors.WithMessage(err, "mark used image ID")
}

func (s *ImageIdStore) idFile(imageId digest.Digest) (string, error) {
	if err := imageId.Validate(); err != nil {
		return "", errors.Errorf("invalid image ID %q: %s", imageId, err)
	}
	return filepath.Join(s.dir, imageId.Algorithm().String()+"-"+imageId.Hex()), nil
}

func decodeImageIdFileName(fileName string) (id digest.Digest, err error) {
	idStr := strings.Replace(fileName, "-", ":", 1)
	if id, err = digest.Parse(idStr); err == nil {
		err = id.Validate()
	}
	if err != nil {
		err = errors.Errorf("decode image ID from file name %s: %s", idStr, err)
	}
	return
}

func readImageIDFile(file string) (imageID digest.Digest, err error) {
	f, err := os.Open(file)
	if err == nil {
		defer f.Close()
		var b []byte
		if b, err = ioutil.ReadAll(f); err == nil {
			imageID, err = digest.Parse(string(b))
		}
	}
	if err != nil {
		err = errors.New("read image ID file: " + err.Error())
	}
	return
}
