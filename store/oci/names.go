package oci

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/mgoltzsche/cntnr/log"
	"github.com/mgoltzsche/cntnr/pkg/atomic"
	digest "github.com/opencontainers/go-digest"
)

var (
	BASE64ENCODER = Base64Encoder("base64-encoder")
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
		return d, fmt.Errorf("key %q does not exist", key)
	}
	f, err := os.Open(file)
	if err != nil {
		return d, fmt.Errorf("get %q: %s", key, err)
	}
	defer f.Close()
	b, err := ioutil.ReadAll(f)
	if err != nil {
		return d, fmt.Errorf("get %q: %s", key, err)
	}
	if d, err = digest.Parse(string(b)); err != nil {
		err = fmt.Errorf("get %q: %s", key, err)
	}
	return
}

func (s *KVStore) Put(key string, value digest.Digest) error {
	file := filepath.Join(s.dir, s.enc.Encode(key))
	if err := os.MkdirAll(filepath.Dir(file), 755); err != nil {
		return fmt.Errorf("put %q -> %q: %s", key, value, err)
	}
	if _, err := atomic.WriteFile(file, bytes.NewReader([]byte(value.String()))); err != nil {
		return fmt.Errorf("put %q -> %q: %s", key, value, err)
	}
	return nil
}

func (s *KVStore) Entries() ([]KVEntry, error) {
	if _, e := os.Stat(s.dir); os.IsNotExist(e) {
		return []KVEntry{}, nil
	}
	fl, err := ioutil.ReadDir(s.dir)
	if err != nil {
		return nil, fmt.Errorf("entries: %s", err)
	}
	l := make([]KVEntry, 0, len(fl))
	for _, f := range fl {
		if f.IsDir() {
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

type Base64Encoder string

func (_ Base64Encoder) Encode(key string) string {
	return base64.RawStdEncoding.EncodeToString([]byte(key))
}

func (_ Base64Encoder) Decode(enc string) (string, error) {
	b, err := base64.RawStdEncoding.DecodeString(enc)
	return string(b), err
}

type PlainEncoder string

func (_ PlainEncoder) Encode(key string) string {
	return key
}

func (_ PlainEncoder) Decode(enc string) (string, error) {
	return enc, nil
}
