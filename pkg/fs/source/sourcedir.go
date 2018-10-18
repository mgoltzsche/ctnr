package source

import (
	"time"

	"github.com/mgoltzsche/ctnr/pkg/fs"
)

var _ fs.Source = &SourceDir{}

type SourceDir struct {
	attrs fs.FileAttrs
}

func NewSourceDir(attrs fs.FileAttrs) fs.Source {
	if attrs.Mtime.IsZero() {
		attrs.Mtime = time.Now().UTC()
	}
	return &SourceDir{attrs}
}

func (s *SourceDir) Attrs() fs.NodeInfo {
	return fs.NodeInfo{fs.TypeDir, s.attrs}
}

func (s *SourceDir) DeriveAttrs() (fs.DerivedAttrs, error) {
	return fs.DerivedAttrs{}, nil
}

func (s *SourceDir) Write(dest, name string, w fs.Writer, _ map[fs.Source]string) error {
	return w.Dir(dest, name, s.attrs)
}

func (s *SourceDir) String() string {
	return "sourceDir{" + s.attrs.String() + "}"
}
