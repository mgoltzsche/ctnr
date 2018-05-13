package files

import (
	"os"

	"github.com/opencontainers/go-digest"
	"github.com/pkg/errors"
)

var _ Source = &sourceFile{}

type sourceFile struct {
	file  string
	attrs FileAttrs
	hash  string
}

func NewSourceFile(file string, attrs FileAttrs) Source {
	return &sourceFile{file, attrs, ""}
}

func (s *sourceFile) Type() SourceType {
	return TypeFile
}
func (s *sourceFile) Attrs() *FileAttrs {
	return &s.attrs
}

func (s *sourceFile) Hash() (string, error) {
	if s.hash == "" {
		f, err := os.Open(s.file)
		if err != nil {
			return "", errors.Wrap(err, "hash")
		}
		defer f.Close()
		d, err := digest.FromReader(f)
		if err != nil {
			return "", errors.Errorf("hash %s: %s", s.file, err)
		}
		s.hash = "file:" + d.String()
	}
	return s.hash, nil
}

func (s *sourceFile) WriteFiles(dest string, w Writer) (err error) {
	f, err := os.Open(s.file)
	if err != nil {
		return errors.New(err.Error())
	}
	defer f.Close()
	return w.File(dest, f, s.attrs)
}
