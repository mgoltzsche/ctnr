package builder

import (
	"path/filepath"

	"github.com/mgoltzsche/cntnr/log"
	digest "github.com/opencontainers/go-digest"
	"github.com/pkg/errors"
)

type ImageBuildCache interface {
	Get(parent digest.Digest, uniqHistoryEntry string) (digest.Digest, error)
	Put(parent digest.Digest, uniqHistoryEntry string, child digest.Digest) error
}

type imageBuildCache struct {
	dir  string
	warn log.FieldLogger
}

func NewImageBuildCache(dir string, warn log.FieldLogger) ImageBuildCache {
	return &imageBuildCache{dir, warn}
}

func (s *imageBuildCache) Get(parent digest.Digest, uniqHistoryEntry string) (child digest.Digest, err error) {
	c := s.cache(parent)
	cached, err := c.Get(uniqHistoryEntry)
	if err != nil {
		if e, ok := err.(CacheError); ok && e.Temporary() {
			return child, err
		} else {
			return child, errors.Wrap(err, "image build cache")
		}
	}
	child, err = digest.Parse(cached)
	if err != nil {
		return child, errors.Wrap(err, "get cached image build step")
	}
	return
}

func (s *imageBuildCache) Put(parent digest.Digest, uniqHistoryEntry string, child digest.Digest) (err error) {
	c := s.cache(parent)
	if err = c.Put(uniqHistoryEntry, child.String()); err != nil {
		err = errors.Wrap(err, "image build cache")
	}
	return
}

func (s *imageBuildCache) cache(image digest.Digest) CacheFile {
	file := filepath.Join(s.dir, image.Algorithm().String(), image.Hex())
	return NewCacheFile(file, s.warn.WithField("image", image.String()))
}
