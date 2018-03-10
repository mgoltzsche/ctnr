package builder

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mgoltzsche/cntnr/pkg/log"
	"github.com/pkg/errors"
)

type CacheFile struct {
	file  string
	cache map[string]string
	warn  log.Logger
}

type CacheError string

func (e CacheError) Error() string {
	return string(e)
}

func (e CacheError) Temporary() bool {
	return true
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
			return
		}
	}
	child, ok := s.cache[key]
	if !ok {
		err = CacheError(fmt.Sprintf("cache key %q not found", key))
	}
	return
}

func (s *CacheFile) read() (idx map[string]string, err error) {
	idx = map[string]string{}
	f, err := os.OpenFile(s.file, os.O_RDONLY, 0664)
	if err != nil {
		if os.IsNotExist(err) {
			return idx, nil
		} else {
			return nil, errors.Wrap(err, "read build cache")
		}
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	i := 0
	for scanner.Scan() {
		if line := scanner.Text(); line != "" {
			entry := cacheEntry{}
			if e := json.Unmarshal([]byte(line), &entry); e != nil {
				s.warn.Printf("read build cache line %d: %s", i, err)
			} else {
				idx[entry.Key] = entry.Value
			}
			i++
		}
	}
	if err = scanner.Err(); err != nil {
		return nil, errors.Wrap(err, "read build cache")
	}
	return
}

func (s *CacheFile) Put(key, value string) (err error) {
	if err = os.MkdirAll(filepath.Dir(s.file), 0775); err != nil {
		return errors.Wrap(err, "put build cache")
	}
	f, err := os.OpenFile(s.file, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0664)
	if err != nil {
		return errors.Wrap(err, "put build cache")
	}
	defer f.Close()
	b, err := json.Marshal(cacheEntry{key, value})
	if err != nil {
		return errors.Wrap(err, "put build cache")
	}
	if _, err = f.Write([]byte(string(b) + "\n")); err != nil {
		return errors.Wrap(err, "put build cache")
	}
	if s.cache != nil {
		s.cache[key] = value
	}
	return
}
