package builder

import (
	"fmt"
	"path/filepath"

	exterrors "github.com/mgoltzsche/ctnr/pkg/errors"
	"github.com/mgoltzsche/ctnr/pkg/fs/source"
	"github.com/mgoltzsche/ctnr/pkg/log"
	digest "github.com/opencontainers/go-digest"
	"github.com/pkg/errors"
)

type ImageBuildCache interface {
	GetCachedImageId(parent *digest.Digest, uniqHistoryEntry string) (digest.Digest, error)
	PutCachedImageId(parent *digest.Digest, uniqHistoryEntry string, child digest.Digest) error
	HttpHeaderCache(image *digest.Digest) source.HttpHeaderCache
	// TODO: add HttpEtagCache
}

type noOpCache string

func (_ noOpCache) GetCachedImageId(parent *digest.Digest, uniqHistoryEntry string) (d digest.Digest, err error) {
	err = exterrors.Typed(errCacheKeyNotExist, "image build cache is disabled")
	return
}

func (_ noOpCache) PutCachedImageId(parent *digest.Digest, uniqHistoryEntry string, child digest.Digest) error {
	return nil
}

func (_ noOpCache) HttpHeaderCache(image *digest.Digest) source.HttpHeaderCache {
	return source.NoopHttpHeaderCache("NoopHttpHeaderCache")
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

func (s *imageBuildCache) GetCachedImageId(parent *digest.Digest, uniqHistoryEntry string) (child digest.Digest, err error) {
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
	return child, errors.Wrap(err, "build cache: get imageId")
}

func (s *imageBuildCache) PutCachedImageId(parent *digest.Digest, uniqHistoryEntry string, child digest.Digest) (err error) {
	c := s.cache(parent, uniqHistoryEntry)
	err = c.Put(uniqHistoryEntry, child.String())
	return errors.Wrap(err, "build cache: put imageId")
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

func (s *imageBuildCache) HttpHeaderCache(image *digest.Digest) source.HttpHeaderCache {
	var dir string
	if image == nil {
		dir = filepath.Join(s.dir, "default")
	} else {
		dir = filepath.Join(s.dir, (*image).Algorithm().String(), (*image).Hex(), "http")
	}
	return source.NewHttpHeaderCache(dir)
}
