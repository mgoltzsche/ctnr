package store

import (
	"io"
	"io/ioutil"
	"os"
	"path/filepath"

	digest "github.com/opencontainers/go-digest"
	"github.com/pkg/errors"
)

type ContentAddressableStore struct {
	BlobStore
}

func NewContentAddressableStore(dir string) (r ContentAddressableStore) {
	return ContentAddressableStore{NewBlobStore(dir)}
}

func (s *ContentAddressableStore) GetInfo(id digest.Digest) (st os.FileInfo, err error) {
	file, err := s.keyFile(id)
	if err != nil {
		return
	}
	if st, err = os.Stat(file); err != nil {
		err = errors.Wrap(err, "blob file info")
	}
	return
}

// Writes a raw blob into the store using its digest as key
func (s *ContentAddressableStore) Put(reader io.Reader) (d digest.Digest, size int64, err error) {
	defer func() {
		err = errors.WithMessage(err, "put blob")
	}()

	// Create blob dir
	blobDir := string(s.BlobStore)
	if err = os.MkdirAll(blobDir, 0775); err != nil {
		err = errors.New(err.Error())
		return
	}
	// Create temp file to write blob to
	tmpBlob, err := ioutil.TempFile(blobDir, "blob-")
	if err != nil {
		err = errors.New(err.Error())
		return
	}
	tmpPath := tmpBlob.Name()
	defer func() {
		tmpBlob.Close()
		if err != nil {
			os.Remove(tmpPath)
		}
	}()

	// Write temp blob
	digester := digest.SHA256.Digester()
	writer := io.MultiWriter(tmpBlob, digester.Hash())
	if size, err = io.Copy(writer, reader); err != nil {
		err = errors.New(err.Error())
		return
	}
	if err = tmpBlob.Sync(); err != nil {
		return
	}
	if err = tmpBlob.Close(); err != nil {
		return
	}

	// Rename temp blob file
	d = digester.Digest()
	blobFile, err := s.keyFile(d)
	if err != nil {
		return
	}
	if _, e := os.Stat(blobFile); e == nil {
		// Do not override existing blob
		os.Remove(tmpPath)
		return
	}
	if err = os.MkdirAll(filepath.Dir(blobFile), 0775); err != nil {
		return
	}
	err = errors.Wrap(os.Rename(tmpPath, blobFile), "put blob")
	return
}
