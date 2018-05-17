package source

import (
	"github.com/mgoltzsche/cntnr/pkg/fs"
)

var _ fs.Source = &sourceFifo{}

type sourceFifo struct {
	attrs fs.DeviceAttrs
}

func NewSourceFifo(attrs fs.DeviceAttrs) *sourceFifo {
	return &sourceFifo{attrs}
}

func (s *sourceFifo) Attrs() fs.NodeInfo {
	return fs.NodeInfo{fs.TypeFifo, s.attrs.FileAttrs}
}

func (s *sourceFifo) Write(path, name string, w fs.Writer, written map[fs.Source]string) (err error) {
	if linkDest, ok := written[s]; ok {
		err = w.Link(path, linkDest)
	} else {
		written[s] = path
		err = w.Fifo(path, s.attrs)
	}
	return
}
func (s *sourceFifo) Equal(o fs.Source) (bool, error) {
	return s.Attrs().Equal(o.Attrs()), nil
}
