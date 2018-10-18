package store

import (
	"io"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/mgoltzsche/ctnr/image"
	"github.com/mgoltzsche/ctnr/pkg/atomic"
	exterrors "github.com/mgoltzsche/ctnr/pkg/errors"
	"github.com/opencontainers/go-digest"
	"github.com/pkg/errors"
)

type BlobStore string

func NewBlobStore(dir string) BlobStore {
	return BlobStore(dir)
}

func (s BlobStore) Keys() (r []digest.Digest, err error) {
	dl, err := ioutil.ReadDir(string(s))
	if err != nil && !os.IsNotExist(err) {
		return r, errors.Wrap(err, "keys")
	}
	if len(dl) > 0 {
		for _, d := range dl {
			if d.IsDir() {
				dir := filepath.Join(string(s), d.Name())
				fl, e := ioutil.ReadDir(dir)
				if e != nil {
					return r, errors.Wrap(e, "keys")
				}
				r = append(make([]digest.Digest, 0, len(r)+len(fl)), r...)
				for _, f := range fl {
					if !f.IsDir() {
						if d, e := digest.Parse(d.Name() + ":" + f.Name()); e == nil {
							r = append(r, d)
						}
					}
				}
			}
		}
	}
	return
}

func (s BlobStore) Exists(key digest.Digest) (r bool, err error) {
	file, err := s.keyFile(key)
	if err != nil {
		return
	}
	if _, e := os.Stat(file); e != nil {
		if os.IsNotExist(e) {
			return false, nil
		} else {
			return false, errors.Wrap(e, "kvstore")
		}
	}
	return true, nil
}

func (s BlobStore) Get(key digest.Digest) (f io.ReadCloser, err error) {
	file, err := s.keyFile(key)
	if err != nil {
		return
	}
	if f, err = os.Open(file); err != nil {
		if os.IsNotExist(err) {
			return nil, image.ErrNotExist(errors.Errorf("kvstore: get: key %s does not exist", key))
		} else {
			return nil, errors.Wrap(err, "kvstore: get")
		}
	}
	return
}

func (s BlobStore) Put(key digest.Digest, content io.Reader) (written int64, err error) {
	file, err := s.keyFile(key)
	if err != nil {
		return
	}
	if err = os.MkdirAll(filepath.Dir(file), 0775); err == nil {
		written, err = atomic.WriteFile(file, content)
	}
	return written, errors.Wrap(err, "kvstore: put")
}

func (s BlobStore) Delete(key digest.Digest) (err error) {
	file, err := s.keyFile(key)
	if err != nil {
		return
	}
	if err = os.Remove(file); err != nil {
		if os.IsNotExist(err) {
			err = image.ErrNotExist(errors.Errorf("kvstore: del: key %s does not exist", key))
		} else {
			err = errors.Wrap(err, "kvstore: del")
		}
	}
	return
}

func (s BlobStore) Retain(keep map[digest.Digest]bool) (err error) {
	defer func() {
		err = errors.Wrap(err, "retain blobs")
	}()
	var (
		al, dl []os.FileInfo
		dir    = s.dir()
	)
	if al, err = ioutil.ReadDir(dir); err != nil {
		if os.IsNotExist(err) {
			return nil
		} else {
			return
		}
	}
	for _, f := range al {
		if f.IsDir() {
			alg := f.Name()
			af := filepath.Join(dir, alg)
			dl, err = ioutil.ReadDir(af)
			if err != nil {
				return
			}
			for _, f = range dl {
				if blobDigest := digest.NewDigestFromHex(alg, f.Name()); blobDigest.Validate() == nil {
					if !keep[blobDigest] {
						if e := os.Remove(filepath.Join(af, f.Name())); e != nil {
							err = exterrors.Append(err, e)
						}
					}
				}
			}
		}
	}
	return
}

func (s BlobStore) dir() string {
	return string(s)
}

func (s BlobStore) keyFile(key digest.Digest) (string, error) {
	if err := key.Validate(); err != nil {
		return "", errors.Wrapf(err, "kvstore: invalid key %q", key)
	}
	return filepath.Join(s.dir(), key.Algorithm().String(), key.Hex()), nil
}
