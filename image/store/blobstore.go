package store

import (
	"compress/gzip"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/mgoltzsche/cntnr/pkg/log"
	digest "github.com/opencontainers/go-digest"
	ispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
)

type BlobStore struct {
	blobDir string
	debug   log.Logger
}

func NewBlobStore(dir string, debug log.Logger) (r BlobStore) {
	r.blobDir = dir
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
	layer.Digest, layer.Size, err = s.putBlob(pipeReader)
	if err != nil {
		return
	}
	diffIdDigest = diffIdDigester.Digest()
	layer.MediaType = ispecs.MediaTypeImageLayerGzip
	return
}

func (s *BlobStore) BlobFileInfo(id digest.Digest) (st os.FileInfo, err error) {
	if st, err = os.Stat(s.blobFile(id)); err != nil {
		err = errors.New(err.Error())
	}
	return
}

func (s *BlobStore) RetainBlobs(keep map[digest.Digest]bool) (err error) {
	defer func() {
		err = errors.Wrap(err, "retain blobs")
	}()
	var al, dl []os.FileInfo
	if al, err = ioutil.ReadDir(s.blobDir); err != nil {
		if os.IsNotExist(err) {
			return nil
		} else {
			return errors.New(err.Error())
		}
	}
	for _, f := range al {
		if f.IsDir() {
			alg := f.Name()
			af := filepath.Join(s.blobDir, alg)
			dl, err = ioutil.ReadDir(af)
			if err != nil {
				return errors.New(err.Error())
			}
			for _, f = range dl {
				if blobDigest := digest.NewDigestFromHex(alg, f.Name()); blobDigest.Validate() == nil {
					if !keep[blobDigest] {
						if e := os.Remove(filepath.Join(af, f.Name())); e != nil {
							err = errors.New(e.Error())
							s.debug.Printf("blob %s: %s", blobDigest, e)
						}
					}
				}
			}
		}
	}
	return
}

func (s *BlobStore) readBlob(id digest.Digest) (b []byte, err error) {
	if err = id.Validate(); err != nil {
		return nil, errors.New("blob digest " + id.String() + ": " + err.Error())
	}
	if b, err = ioutil.ReadFile(filepath.Join(s.blobDir, id.Algorithm().String(), id.Hex())); err != nil {
		err = errors.New("read blob " + id.String() + ": " + err.Error())
	}
	return
}

func (s *BlobStore) putBlob(reader io.Reader) (d digest.Digest, size int64, err error) {
	defer func() {
		err = errors.WithMessage(err, "put blob")
	}()

	// Create blob dir
	if err = os.MkdirAll(s.blobDir, 0775); err != nil {
		err = errors.New(err.Error())
		return
	}
	// Create temp file to write blob to
	tmpBlob, err := ioutil.TempFile(s.blobDir, "blob-")
	if err != nil {
		err = errors.New(err.Error())
		return
	}
	tmpPath := tmpBlob.Name()
	defer tmpBlob.Close()

	// Write temp blob
	digester := digest.SHA256.Digester()
	writer := io.MultiWriter(tmpBlob, digester.Hash())
	if size, err = io.Copy(writer, reader); err != nil {
		err = errors.New(err.Error())
		return
	}
	tmpBlob.Close()

	// Rename temp blob file
	d = digester.Digest()
	blobFile := s.blobFile(d)
	if _, e := os.Stat(blobFile); os.IsNotExist(e) {
		// Write blob if not exists
		err = os.Rename(tmpPath, blobFile)
	} else {
		// Do not override already existing blob to make hash collisions obvious early
		err = os.Remove(tmpPath)
	}
	if err != nil {
		err = errors.New(err.Error())
	}
	return
}

func (s *BlobStore) blobFile(d digest.Digest) string {
	return filepath.Join(s.blobDir, d.Algorithm().String(), d.Hex())
}
