package store

import (
	"compress/gzip"
	"io"
	"io/ioutil"
	"os"

	"github.com/mgoltzsche/cntnr/pkg/log"
	digest "github.com/opencontainers/go-digest"
	ispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
)

type BlobStore struct {
	KVFileStore
	debug log.Logger
}

func NewBlobStore(dir string, debug log.Logger) (r BlobStore) {
	r.KVFileStore = NewKVFileStore(dir)
	r.debug = debug
	return
}

func (s *BlobStore) PutLayer(reader io.Reader) (layer ispecs.Descriptor, diffIdDigest digest.Digest, err error) {
	// diffID digest
	diffIdDigester := digest.SHA256.Digester()
	hashReader := io.TeeReader(reader, diffIdDigester.Hash())
	pipeReader, pipeWriter := io.Pipe()
	defer pipeReader.Close()

	// gzip
	gzw := gzip.NewWriter(pipeWriter)
	defer gzw.Close()
	go func() {
		if _, err := io.Copy(gzw, hashReader); err != nil {
			pipeWriter.CloseWithError(errors.Wrap(err, "compressing layer blob"))
			return
		}
		gzw.Close()
		pipeWriter.Close()
	}()

	// Write blob
	layer.Digest, layer.Size, err = s.Put(pipeReader)
	if err != nil {
		return
	}
	diffIdDigest = diffIdDigester.Digest()
	layer.MediaType = ispecs.MediaTypeImageLayerGzip
	return
}

func (s *BlobStore) BlobFileInfo(id digest.Digest) (st os.FileInfo, err error) {
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
func (s *BlobStore) Put(reader io.Reader) (d digest.Digest, size int64, err error) {
	defer func() {
		err = errors.WithMessage(err, "put blob")
	}()

	// Create blob dir
	blobDir := string(s.KVFileStore)
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
	err = errors.Wrap(os.Rename(tmpPath, blobFile), "put blob")
	return
}
