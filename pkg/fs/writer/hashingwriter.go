package writer

import (
	"io"

	"github.com/mgoltzsche/ctnr/pkg/fs"
	"github.com/opencontainers/go-digest"
	//	"github.com/pkg/errors"
)

type HashingWriter struct {
	fs.Writer
}

func NewHashingWriter(delegate fs.Writer) fs.Writer {
	return &HashingWriter{delegate}
}

func (w *HashingWriter) Lazy(path, name string, src fs.LazySource, written map[fs.Source]string) (err error) {
	return src.Expand(path, w, written)
}

func (w *HashingWriter) File(path string, src fs.FileSource) (fs.Source, error) {
	return w.Writer.File(path, &hashingSource{src, ""})
}

type hashingSource struct {
	fs.FileSource
	hash string
}

func (s *hashingSource) HashIfAvailable() string {
	return s.hash
}

func (s *hashingSource) DeriveAttrs() (a fs.DerivedAttrs, err error) {
	if a, err = s.FileSource.DeriveAttrs(); err == nil {
		a.Hash = s.hash
	}
	return
}

func (s *hashingSource) Reader() (r io.ReadCloser, err error) {
	if r, err = s.FileSource.Reader(); err != nil || s.hash != "" {
		return
	}
	digester := digest.SHA256.Digester()
	tr := io.TeeReader(r, digester.Hash())
	return &hashAssigningReadCloser{tr, r, digester, &s.hash}, nil
}

type hashAssigningReadCloser struct {
	io.Reader
	closer   io.Closer
	digester digest.Digester
	hash     *string
}

func (r *hashAssigningReadCloser) Close() (err error) {
	if err = r.closer.Close(); err == nil {
		*r.hash = r.digester.Digest().String()
	}
	return
}
