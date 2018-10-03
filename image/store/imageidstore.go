package store

import (
	"io/ioutil"
	"strings"

	"github.com/opencontainers/go-digest"
	"github.com/pkg/errors"
)

type ImageIdStore struct {
	KVFileStore
}

type ImageID struct {
	ID             digest.Digest
	ManifestDigest digest.Digest
}

func (s ImageID) String() string {
	return s.ID.String() + " -> " + s.ManifestDigest.String()
}

func NewImageIdStore(dir string) ImageIdStore {
	return ImageIdStore{NewKVFileStore(dir)}
}

func (s *ImageIdStore) Put(imageID, manifestDigest digest.Digest) (err error) {
	defer func() {
		err = errors.Wrapf(err, "imageidstore: put %s -> %s", imageID, manifestDigest)
	}()
	if err = manifestDigest.Validate(); err != nil {
		return errors.Errorf("invalid manifest digest")
	}
	_, err = s.KVFileStore.Put(imageID, strings.NewReader(manifestDigest.String()))
	return
}

func (s *ImageIdStore) Get(imageID digest.Digest) (r ImageID, err error) {
	r.ID = imageID
	reader, err := s.KVFileStore.Get(imageID)
	if err == nil {
		if b, err := ioutil.ReadAll(reader); err == nil {
			r.ManifestDigest, err = digest.Parse(string(b))
		}
	}
	return r, errors.Wrap(err, "imageidstore")
}

func (s *ImageIdStore) Entries() (r []ImageID, err error) {
	imageIds, err := s.KVFileStore.Keys()
	if err != nil {
		return
	}
	r = make([]ImageID, 0, len(imageIds))
	for _, imgId := range imageIds {
		entry, e := s.Get(imgId)
		if e != nil {
			return r, errors.Wrap(e, "image IDs")
		}
		r = append(r, entry)
	}
	return
}
