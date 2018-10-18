package source

import (
	"github.com/mgoltzsche/ctnr/pkg/fs"
)

type sourceWhiteout string

func NewSourceWhiteout() fs.Source {
	return sourceWhiteout(fs.TypeWhiteout)
}

func (s sourceWhiteout) Attrs() fs.NodeInfo {
	return fs.NodeInfo{NodeType: fs.TypeWhiteout}
}

func (s sourceWhiteout) DeriveAttrs() (a fs.DerivedAttrs, err error) {
	return fs.DerivedAttrs{}, nil
}

func (s sourceWhiteout) Write(dest, name string, w fs.Writer, written map[fs.Source]string) error {
	return w.Remove(dest)
}
