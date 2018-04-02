package store

import (
	"bytes"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"

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
	if err = imageID.Validate(); err != nil {
		return errors.New("invalid image ID: " + imageID.String())
	}
	if err = manifestDigest.Validate(); err != nil {
		return errors.New("invalid manifest digest: " + manifestDigest.String())
	}
	if err = os.MkdirAll(filepath.Join(s.dir), 0775); err != nil {
		return errors.New(err.Error())
	}
	file := s.idFile(imageID)
	_, err = atomic.WriteFile(file, bytes.NewBufferString(manifestDigest.String()))
	return
}

func (s *ImageIdStore) Del(imageID digest.Digest) (err error) {
	if err = os.Remove(s.idFile(imageID)); err != nil {
		err = errors.New("delete image ID " + imageID.String() + ": " + err.Error())
	}
	return
}

func (s *ImageIdStore) ImageID(imageID digest.Digest) (r ImageID, err error) {
	r.ID = imageID
	file := s.idFile(imageID)
	f, err := os.Stat(file)
	if err == nil {
		r.LastUsed = f.ModTime()
		r.ManifestDigest, err = readImageIDFile(file)
	} else {
		err = errors.New("image ID " + imageID.String() + ": " + err.Error())
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
	now := time.Now()
	if err = os.Chtimes(s.idFile(id), now, now); err != nil {
		err = errors.New("mark used image ID: " + err.Error())
	}
	return
}

func (s *ImageIdStore) idFile(imageId digest.Digest) string {
	return filepath.Join(s.dir, imageId.Algorithm().String()+"-"+imageId.Hex())
}

func decodeImageIdFileName(fileName string) (id digest.Digest, err error) {
	idStr := strings.Replace(fileName, "-", ":", 1)
	if id, err = digest.Parse(idStr); err == nil {
		err = id.Validate()
	}
	if err != nil {
		err = errors.New("decode image ID from file name " + idStr + ": " + err.Error())
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
