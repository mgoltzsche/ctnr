package source

import (
	"github.com/mgoltzsche/ctnr/pkg/fs"
)

type SourceUpperLink struct {
	fs.Source
}

func NewSourceUpperLink(s fs.Source) fs.Source {
	return &SourceUpperLink{s}
}

func (f *SourceUpperLink) Write(path, name string, w fs.Writer, written map[fs.Source]string) (err error) {
	if linkDest, ok := written[f.Source]; ok {
		err = w.Link(path, linkDest)
	} else {
		err = f.Source.Write(path, name, w, written)
	}
	return
}
