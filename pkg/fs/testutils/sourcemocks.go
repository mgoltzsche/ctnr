package testutils

import (
	"path/filepath"

	"github.com/mgoltzsche/cntnr/pkg/fs"
)

type SourceMock struct {
	fs.NodeAttrs
	fs.Readable
	Err error
}

func NewSourceMock(t fs.NodeType, a fs.FileAttrs, hash string) *SourceMock {
	r := SourceMock{fs.NodeAttrs{fs.NodeInfo{t, a}, fs.DerivedAttrs{Hash: hash}}, fs.NewReadableBytes([]byte("mockcontent")), nil}
	return &r
}

func (s *SourceMock) Attrs() fs.NodeInfo { return s.NodeInfo }
func (s *SourceMock) DeriveAttrs() (fs.DerivedAttrs, error) {
	return s.DerivedAttrs, s.Err
}
func (s *SourceMock) Write(dest, name string, w fs.Writer, written map[fs.Source]string) (err error) {
	a := s.NodeAttrs
	t := a.NodeType
	if t == fs.TypeDir {
		err = w.Dir(dest, name, a.FileAttrs)
	} else if linkDest, ok := written[s]; ok {
		err = w.Link(dest, linkDest)
	} else if t == fs.TypeSymlink {
		written[s] = dest
		err = w.Symlink(dest, a.FileAttrs)
	} else {
		written[s] = dest
		_, err = w.File(dest, s)
	}
	if s.Err != nil {
		err = s.Err
	}
	return err
}
func (s *SourceMock) HashIfAvailable() string {
	return s.Hash
}
func (s *SourceMock) String() string {
	return "sourceMock{" + s.NodeAttrs.String() + "}"
}

type SourceOverlayMock struct {
	*SourceMock
}

func (s *SourceOverlayMock) Write(dest, name string, w fs.Writer, written map[fs.Source]string) error {
	return w.Lazy(dest, name, s, written)
}

func (s *SourceOverlayMock) Expand(dest string, w fs.Writer, _ map[fs.Source]string) (err error) {
	a := s.Attrs().FileAttrs
	a.Mode = 0644
	src := NewSourceMock(fs.TypeFile, a, "")
	src.Readable = fs.NewReadableBytes([]byte("content"))
	_, err = w.File(filepath.Join(dest, "xtracted"), src)
	return
}

func (s *SourceOverlayMock) String() string {
	return "sourceOverlayMock{" + s.SourceMock.String() + "}"
}
