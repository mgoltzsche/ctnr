package builder

import (
	"fmt"
	"path/filepath"

	exterrors "github.com/mgoltzsche/cntnr/pkg/errors"
	"github.com/mgoltzsche/cntnr/pkg/log"
	digest "github.com/opencontainers/go-digest"
	"github.com/pkg/errors"
)

type ImageBuildCache interface {
	Get(parent *digest.Digest, uniqHistoryEntry string) (digest.Digest, error)
	Put(parent *digest.Digest, uniqHistoryEntry string, child digest.Digest) error
}

type noOpCache string

func (_ noOpCache) Get(parent *digest.Digest, uniqHistoryEntry string) (d digest.Digest, err error) {
	err = exterrors.Typed(errCacheKeyNotExist, "image build cache is disabled")
	return
}

func (_ noOpCache) Put(parent *digest.Digest, uniqHistoryEntry string, child digest.Digest) error {
	return nil
}

func NewNoOpCache() ImageBuildCache {
	return noOpCache("image build cache disabled")
}

type imageBuildCache struct {
	dir  string
	warn log.FieldLogger
}

func NewImageBuildCache(dir string, warn log.FieldLogger) ImageBuildCache {
	return &imageBuildCache{dir, warn}
}

func (s *imageBuildCache) Get(parent *digest.Digest, uniqHistoryEntry string) (child digest.Digest, err error) {
	c := s.cache(parent, uniqHistoryEntry)
	cached, err := c.Get(uniqHistoryEntry)
	if err == nil {
		if child, err = digest.Parse(cached); err == nil {
			if err = child.Validate(); err != nil {
				msg := fmt.Sprintf("invalid cache value %q found in %s: %s", child, c.file, err)
				s.warn.Println(msg)
				err = exterrors.Typed(errCacheKeyNotExist, msg)
				child = digest.Digest("")
			}
		}
	}
	return child, errors.Wrap(err, "get cached image")
}

func (s *imageBuildCache) Put(parent *digest.Digest, uniqHistoryEntry string, child digest.Digest) (err error) {
	c := s.cache(parent, uniqHistoryEntry)
	err = c.Put(uniqHistoryEntry, child.String())
	return errors.Wrap(err, "image build cache")
}

func (s *imageBuildCache) cache(image *digest.Digest, uniqHistoryEntry string) CacheFile {
	var file string
	d := digest.SHA256.FromString(uniqHistoryEntry)
	warn := s.warn
	if image == nil {
		file = filepath.Join(s.dir, "default", uniqHistoryEntry, d.Algorithm().String(), d.Hex())
	} else {
		warn = s.warn.WithField("image", image.String())
		file = filepath.Join(s.dir, (*image).Algorithm().String(), (*image).Hex(), d.Algorithm().String(), d.Hex())
	}
	return NewCacheFile(file, warn)
}
