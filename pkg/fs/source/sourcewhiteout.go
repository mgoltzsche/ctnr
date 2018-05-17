package source

import (
	"github.com/mgoltzsche/cntnr/pkg/fs"
)

type sourceWhiteout string

func NewSourceWhiteout() fs.Source {
	return sourceWhiteout(fs.TypeWhiteout)
}

func (s sourceWhiteout) Equal(o fs.Source) (bool, error) {
	return o.Attrs().NodeType == fs.TypeWhiteout, nil
}

func (s sourceWhiteout) Attrs() fs.NodeInfo {
	return fs.NodeInfo{NodeType: fs.TypeWhiteout}
}

func (s sourceWhiteout) DerivedAttrs() (a fs.NodeAttrs, err error) {
	return
}

func (s sourceWhiteout) Write(dest, name string, w fs.Writer, written map[fs.Source]string) error {
	return w.Remove(dest)
}
