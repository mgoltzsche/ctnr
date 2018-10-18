package builder

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"

	exterrors "github.com/mgoltzsche/ctnr/pkg/errors"
	"github.com/mgoltzsche/ctnr/pkg/log"
	"github.com/pkg/errors"
)

const errCacheKeyNotExist = "github.com/mgoltzsche/ctnr/image/builder/cache/notexist"

func IsCacheKeyNotExist(err error) bool {
	return exterrors.HasType(err, errCacheKeyNotExist)
}

type CacheFile struct {
	file  string
	cache map[string]string
	warn  log.Logger
}

type cacheEntry struct {
	Key   string
	Value string
}

func NewCacheFile(file string, warn log.Logger) CacheFile {
	return CacheFile{file, nil, warn}
}

func (s *CacheFile) Get(key string) (child string, err error) {
	if s.cache == nil {
		if s.cache, err = s.read(); err != nil {
			return "", errors.Wrapf(err, "get cache key %q", key)
		}
	}
	child, ok := s.cache[key]
	if !ok {
		err = exterrors.Typedf(errCacheKeyNotExist, "cache key %q not found", key)
	}
	return
}

func (s *CacheFile) read() (idx map[string]string, err error) {
	idx = map[string]string{}
	f, err := os.OpenFile(s.file, os.O_RDONLY, 0660)
	if err != nil {
		if _, e := os.Stat(s.file); os.IsNotExist(e) {
			return idx, nil
		} else {
			return nil, errors.Wrap(err, "read cache")
		}
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	i := 0
	for scanner.Scan() {
		if line := scanner.Text(); line != "" {
			entry := cacheEntry{}
			if e := json.Unmarshal([]byte(line), &entry); e != nil {
				s.warn.Printf("read cache file %s line %d: %s", s.file, i, err)
			} else {
				idx[entry.Key] = entry.Value
			}
			i++
		}
	}
	if err = scanner.Err(); err != nil {
		return nil, errors.Wrap(err, "read cache")
	}
	return
}

func (s *CacheFile) Put(key, value string) (err error) {
	if err = os.MkdirAll(filepath.Dir(s.file), 0775); err == nil {
		var f *os.File
		if f, err = os.OpenFile(s.file, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0660); err == nil {
			defer f.Close()
			var b []byte
			if b, err = json.Marshal(cacheEntry{key, value}); err == nil {
				if _, err = f.Write([]byte(string(b) + "\n")); err == nil {
					err = f.Sync()
				}
			}
		}
	}
	if err != nil {
		return errors.Errorf("put cache %q => %q: %s", key, value, err)
	}
	if s.cache != nil {
		s.cache[key] = value
	}
	return
}
