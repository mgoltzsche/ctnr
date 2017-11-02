package store

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"

	"github.com/mgoltzsche/cntnr/log"
	"github.com/openSUSE/umoci/oci/layer"
	digest "github.com/opencontainers/go-digest"
	ispecs "github.com/opencontainers/image-spec/specs-go/v1"
)

type BlobStore struct {
	blobDir string
	debug   log.Logger
}

func NewBlobStore(dir string, debug log.Logger) (r BlobStore, err error) {
	r.blobDir = dir
	r.debug = debug
	if err = os.MkdirAll(dir, 0755); err != nil {
		err = fmt.Errorf("init blob store: %s", err)
	}
	return
}

func (s *BlobStore) ImageManifest(manifestDigest digest.Digest) (r ispecs.Manifest, err error) {
	b, err := s.readBlob(manifestDigest)
	if err != nil {
		return r, fmt.Errorf("image manifest: %s", err)
	}
	if err = json.Unmarshal(b, &r); err != nil {
		err = fmt.Errorf("unmarshal image manifest %q: %s", manifestDigest.String(), err)
	}
	return
}

func (s *BlobStore) PutImageManifest(m ispecs.Manifest) (d ispecs.Descriptor, err error) {
	d.Digest, d.Size, err = s.putJsonBlob(m)
	d.MediaType = ispecs.MediaTypeImageManifest
	d.Platform = &ispecs.Platform{
		Architecture: runtime.GOARCH,
		OS:           runtime.GOOS,
	}
	if err != nil {
		err = fmt.Errorf("put image manifest: %s", err)
	}
	return
}

func (s *BlobStore) ImageConfig(configDigest digest.Digest) (r ispecs.Image, err error) {
	b, err := s.readBlob(configDigest)
	if err != nil {
		return r, fmt.Errorf("image config: %s", err)
	}
	if err = json.Unmarshal(b, &r); err != nil {
		err = fmt.Errorf("unmarshal image config: %s", err)
	}
	return
}

func (s *BlobStore) PutImageConfig(m ispecs.Image) (d ispecs.Descriptor, err error) {
	d.Digest, d.Size, err = s.putJsonBlob(m)
	d.MediaType = ispecs.MediaTypeImageConfig
	if err != nil {
		err = fmt.Errorf("put image config: %s", err)
	}
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
			pipeWriter.CloseWithError(fmt.Errorf("compressing layer: %s", err))
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

func (s *BlobStore) BlobFileInfo(id digest.Digest) (os.FileInfo, error) {
	return os.Stat(s.blobFile(id))
}

func (s *BlobStore) RetainBlobs(keep map[digest.Digest]bool) (err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("retain blobs: %s", err)
		}
	}()
	var al, dl []os.FileInfo
	al, err = ioutil.ReadDir(s.blobDir)
	if err != nil {
		return
	}
	for _, f := range al {
		if f.IsDir() {
			alg := f.Name()
			af := filepath.Join(s.blobDir, alg)
			dl, err = ioutil.ReadDir(af)
			if err != nil {
				return
			}
			for _, f = range dl {
				if blobDigest := digest.NewDigestFromHex(alg, f.Name()); blobDigest.Validate() == nil {
					if !keep[blobDigest] {
						if e := os.Remove(filepath.Join(af, f.Name())); e != nil {
							s.debug.Printf("warn: blob %s: %s", blobDigest, e)
							err = e
						}
					}
				}
			}
		}
	}
	return
}

func (s *BlobStore) unpackLayers(manifest *ispecs.Manifest, dest string) (err error) {
	// Create destination directory
	if err = os.Mkdir(dest, 0755); err != nil {
		return fmt.Errorf("unpack layers: %s", err)
	}
	defer func() {
		if err != nil {
			os.RemoveAll(dest)
		}
	}()

	// Unpack layers
	for _, l := range manifest.Layers {
		d := l.Digest
		s.debug.Printf("Extracting layer %s", d.String())
		layerFile := filepath.Join(s.blobDir, d.Algorithm().String(), d.Hex())
		f, err := os.Open(layerFile)
		if err != nil {
			return fmt.Errorf("unpack layer: %s", err)
		}
		defer f.Close()
		var reader io.Reader
		reader, err = gzip.NewReader(f)
		if err != nil {
			return fmt.Errorf("unpack layer: %s", err)
		}
		if err = layer.UnpackLayer(dest, reader, &layer.MapOptions{Rootless: true}); err != nil {
			return fmt.Errorf("unpack layer: %s", err)
		}
	}
	return
}

func (s *BlobStore) readBlob(id digest.Digest) (b []byte, err error) {
	if err = id.Validate(); err != nil {
		return nil, fmt.Errorf("blob digest %q: %s", id.String(), err)
	}
	b, err = ioutil.ReadFile(filepath.Join(s.blobDir, id.Algorithm().String(), id.Hex()))
	if err != nil {
		err = fmt.Errorf("read blob %s: %s", id, err)
	}
	return
}

func (s *BlobStore) putBlob(reader io.Reader) (d digest.Digest, size int64, err error) {
	// Create temp file to write blob to
	tmpBlob, err := ioutil.TempFile(s.blobDir, "blob-")
	if err != nil {
		err = fmt.Errorf("create temporary blob: %s", err)
		return
	}
	tmpPath := tmpBlob.Name()
	defer tmpBlob.Close()

	// Write temp blob
	digester := digest.SHA256.Digester()
	writer := io.MultiWriter(tmpBlob, digester.Hash())
	if size, err = io.Copy(writer, reader); err != nil {
		err = fmt.Errorf("copy to temporary blob: %s", err)
		return
	}
	tmpBlob.Close()

	// Rename temp blob file
	d = digester.Digest()
	blobFile := s.blobFile(d)
	if _, e := os.Stat(blobFile); os.IsNotExist(e) {
		// Write blob if not exists
		if err = os.Rename(tmpPath, blobFile); err != nil {
			err = fmt.Errorf("rename temp blob: %s", err)
		}
	} else {
		// Do not override already existing blob to make hash collisions obvious early
		if err = os.Remove(tmpPath); err != nil {
			err = fmt.Errorf("remove temp blob: %s", err)
		}
	}
	return
}

func (s *BlobStore) putJsonBlob(o interface{}) (d digest.Digest, size int64, err error) {
	var buf bytes.Buffer
	if err = json.NewEncoder(&buf).Encode(o); err != nil {
		return
	}
	return s.putBlob(&buf)
}

func (s *BlobStore) blobFile(d digest.Digest) string {
	return filepath.Join(s.blobDir, d.Algorithm().String(), d.Hex())
}
