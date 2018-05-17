package source

import (
	"github.com/mgoltzsche/cntnr/pkg/fs"
)

var _ fs.Source = &sourceDevice{}

type sourceDevice struct {
	attrs fs.DeviceAttrs
}

func NewSourceBlock(attrs fs.DeviceAttrs) fs.Source {
	return &sourceDevice{attrs}
}

func (s *sourceDevice) Attrs() fs.NodeInfo {
	return fs.NodeInfo{fs.TypeDevice, s.attrs.FileAttrs}
}

func (s *sourceDevice) Write(path, name string, w fs.Writer, written map[fs.Source]string) (err error) {
	if linkDest, ok := written[s]; ok {
		err = w.Link(path, linkDest)
	} else {
		written[s] = path
		err = w.Device(path, s.attrs)
	}
	return
}

func (s *sourceDevice) Equal(o fs.Source) (bool, error) {
	return s.Attrs().Equal(o.Attrs()), nil
}
