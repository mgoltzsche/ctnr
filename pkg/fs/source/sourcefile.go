package source

import (
	"github.com/mgoltzsche/cntnr/pkg/fs"
	"github.com/opencontainers/go-digest"
	"github.com/pkg/errors"
)

var _ fs.Source = &sourceFile{}

type sourceFile struct {
	fs.Readable
	attrs fs.NodeAttrs
}

func NewSourceFile(reader fs.Readable, attrs fs.FileAttrs) *sourceFile {
	return NewSourceFileHashed(reader, attrs, "")
}

func NewSourceFileHashed(reader fs.Readable, attrs fs.FileAttrs, hash string) *sourceFile {
	return &sourceFile{reader, fs.NodeAttrs{fs.NodeInfo{fs.TypeFile, attrs}, fs.DerivedAttrs{Hash: hash}}}
}

func (s *sourceFile) Attrs() fs.NodeInfo {
	return s.attrs.NodeInfo
}

func (s *sourceFile) HashIfAvailable() string {
	return s.attrs.Hash
}

func (s *sourceFile) Hash() (h string, err error) {
	if s.attrs.Hash == "" {
		f, err := s.Reader()
		if err != nil {
			return "", errors.Wrap(err, "hash")
		}
		defer func() {
			if e := f.Close(); e != nil && err == nil {
				err = e
			}
		}()
		d, err := digest.FromReader(f)
		if err != nil {
			return "", errors.Errorf("hash %s: %s", s.Readable, err)
		}
		s.attrs.Hash = d.String()
	}
	return s.attrs.Hash, nil
}

func (s *sourceFile) DeriveAttrs() (fs.DerivedAttrs, error) {
	_, err := s.Hash()
	return s.attrs.DerivedAttrs, err
}

func (s *sourceFile) Write(path, name string, w fs.Writer, written map[fs.Source]string) (err error) {
	if linkDest, ok := written[s]; ok {
		err = w.Link(path, linkDest)
	} else {
		written[s] = path
		_, err = w.File(path, s)
	}
	return err
}

func (s *sourceFile) Equal(o fs.Source) (bool, error) {
	if !s.attrs.NodeInfo.Equal(o.Attrs()) {
		return false, nil
	}
	oa, err := o.DeriveAttrs()
	if err != nil {
		return false, errors.Wrap(err, "equal")
	}
	a, err := s.DeriveAttrs()
	return a.Equal(&oa), errors.Wrap(err, "equal")
}
