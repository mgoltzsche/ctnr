package source

import (
	"github.com/mgoltzsche/ctnr/pkg/fs"
)

var _ fs.Source = &sourceDevice{}

type sourceDevice struct {
	attrs fs.DeviceAttrs
}

func NewSourceDevice(attrs fs.DeviceAttrs) fs.Source {
	return &sourceDevice{attrs}
}

func (s *sourceDevice) Attrs() fs.NodeInfo {
	return fs.NodeInfo{fs.TypeDevice, s.attrs.FileAttrs}
}

func (s *sourceDevice) DeriveAttrs() (fs.DerivedAttrs, error) {
	return fs.DerivedAttrs{}, nil
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
