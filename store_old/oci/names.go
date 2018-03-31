package oci

import (
	"bytes"
	"encoding/base32"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/mgoltzsche/cntnr/pkg/atomic"
	"github.com/mgoltzsche/cntnr/pkg/log"
	digest "github.com/opencontainers/go-digest"
)

var (
	BASE32ENCODER = Base32Encoder("base32-encoder")
	PLAINENCODER  = PlainEncoder("plain-encoder")
)

type KeyEncoder interface {
	Encode(key string) string
	Decode(value string) (string, error)
}

type KVStore struct {
	dir   string
	enc   KeyEncoder
	debug log.Logger
}

func NewKVStore(dir string, enc KeyEncoder, debug log.Logger) *KVStore {
	return &KVStore{dir, enc, debug}
}

func (s *KVStore) Get(key string) (d digest.Digest, err error) {
	file := filepath.Join(s.dir, s.enc.Encode(key))
	if _, e := os.Stat(file); os.IsNotExist(e) {
		return d, errors.Errorf("key %q does not exist", key)
	}
	f, err := os.Open(file)
	if err != nil {
		return d, errors.Wrapf(err, "get %q", key)
	}
	defer f.Close()
	b, err := ioutil.ReadAll(f)
	if err != nil {
		return d, errors.Wrapf(err, "get %q", key)
	}
	if d, err = digest.Parse(string(b)); err != nil {
		err = errors.Wrapf(err, "get %q", key)
	}
	return
}

func (s *KVStore) Put(key string, value digest.Digest) error {
	file := filepath.Join(s.dir, s.enc.Encode(key))
	if err := os.MkdirAll(filepath.Dir(file), 755); err == nil {
		_, err := atomic.WriteFile(file, bytes.NewReader([]byte(value.String())))
	}
	return errors.Wrapf(err, "put %q -> %q", key, value)
}

func (s *KVStore) Entries() ([]KVEntry, error) {
	if _, e := os.Stat(s.dir); os.IsNotExist(e) {
		return []KVEntry{}, nil
	}
	fl, err := ioutil.ReadDir(s.dir)
	if err != nil {
		return nil, errors.Wrap(err, "entries")
	}
	l := make([]KVEntry, 0, len(fl))
	for _, f := range fl {
		if !f.IsDir() {
			key, err := s.enc.Decode(f.Name())
			if err == nil {
				value, err := s.Get(key)
				if err == nil {
					l = append(l, KVEntry{key, value})
				} else {
					s.debug.Printf("invalid entry %q: %s", key, err)
				}
			} else {
				s.debug.Printf("invalid entry %q: %s", f.Name(), err)
			}
		}
	}
	return l, nil
}

type KVEntry struct {
	Key   string
	Value digest.Digest
}

type Base32Encoder string

func (_ Base32Encoder) Encode(key string) string {
	return base32.StdEncoding.EncodeToString([]byte(key))
}

func (_ Base32Encoder) Decode(enc string) (string, error) {
	b, err := base32.StdEncoding.DecodeString(enc)
	return string(b), err
}

type PlainEncoder string

func (_ PlainEncoder) Encode(key string) string {
	return key
}

func (_ PlainEncoder) Decode(enc string) (string, error) {
	return enc, nil
}
