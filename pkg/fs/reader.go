package fs

import (
	"bytes"
	"io"

	"github.com/openSUSE/umoci/pkg/fseval"
	"github.com/pkg/errors"
)

type fileReader struct {
	file   string
	fsEval fseval.FsEval
}

func NewFileReader(file string, fsEval fseval.FsEval) *fileReader {
	return &fileReader{file, fsEval}
}

func (f *fileReader) Reader() (r io.ReadCloser, err error) {
	r, err = f.fsEval.Open(f.file)
	err = errors.Wrap(err, "file reader")
	return
}

func (f *fileReader) String() string {
	return f.file
}

type readable struct {
	r    io.Reader
	read bool
}

func NewReadable(r io.Reader) Readable {
	return &readable{r, false}
}

func (r *readable) Reader() (io.ReadCloser, error) {
	if r.read {
		return nil, errors.Errorf("reader: already read")
	}
	r.read = true
	return r, nil
}

func (r *readable) Read(p []byte) (int, error) {
	return r.r.Read(p)
}

func (_ *readable) Close() error {
	return nil
}

type readablebytes struct {
	r *bytes.Reader
}

func NewReadableBytes(b []byte) Readable {
	return &readablebytes{bytes.NewReader(b)}
}

func (r *readablebytes) Reader() (io.ReadCloser, error) {
	r.r.Seek(0, 0)
	return r, nil
}

func (r *readablebytes) Read(p []byte) (int, error) {
	return r.r.Read(p)
}

func (_ *readablebytes) Close() error {
	return nil
}
