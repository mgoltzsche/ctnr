package writer

import (
	"io"
	"net/url"
	"path/filepath"

	"github.com/mgoltzsche/cntnr/pkg/fs"
	"github.com/pkg/errors"
)

var _ fs.Writer = &StringWriter{}

type StringWriter struct {
	writer io.Writer
	attrs  fs.AttrSet
}

func NewStringWriter(writer io.Writer, attrs fs.AttrSet) (w *StringWriter) {
	return &StringWriter{writer, attrs}
}

func (w *StringWriter) Parent() (err error) {
	_, err = w.writer.Write([]byte("..\n"))
	return
}

func (w *StringWriter) writeEntry(path, attrs string) (err error) {
	path = filepath.Base(path)
	if path == string(filepath.Separator) {
		path = "."
	}
	_, err = w.writer.Write([]byte(url.PathEscape(path) + " " + attrs + "\n"))
	return
}

func (w *StringWriter) writeAttrs(path string, attrs *fs.NodeAttrs) (err error) {
	return w.writeEntry(path, attrs.AttrString(w.attrs))
}

func (w *StringWriter) LowerNode(path, name string, a *fs.NodeAttrs) (err error) {
	return w.writeAttrs(name, a)
}

func (w *StringWriter) Lazy(path, name string, src fs.LazySource, _ map[fs.Source]string) (err error) {
	var a fs.DerivedAttrs
	if w.attrs&fs.AttrsHash != 0 {
		if a, err = src.DeriveAttrs(); err != nil {
			return errors.Wrapf(err, "stringwriter: lazy %s", path)
		}
	}
	return w.writeAttrs(name, &fs.NodeAttrs{src.Attrs(), a})
}

func (w *StringWriter) File(path string, src fs.FileSource) (r fs.Source, err error) {
	var a fs.DerivedAttrs
	if w.attrs&fs.AttrsHash != 0 {
		if a, err = src.DeriveAttrs(); err != nil {
			return nil, errors.Wrapf(err, "stringwriter: file %s", path)
		}
	}
	err = w.writeAttrs(path, &fs.NodeAttrs{src.Attrs(), a})
	return src, err
}

func (w *StringWriter) Link(path, target string) (err error) {
	return w.writeEntry(path, "hlink="+target)
}

func (w *StringWriter) LowerLink(path, target string, a *fs.NodeAttrs) (err error) {
	return w.Link(path, target)
}

func (w *StringWriter) Symlink(path string, a fs.FileAttrs) (err error) {
	return w.writeAttrs(path, &fs.NodeAttrs{fs.NodeInfo{fs.TypeSymlink, a}, fs.DerivedAttrs{}})
}

func (w *StringWriter) Fifo(path string, a fs.DeviceAttrs) (err error) {
	return w.writeAttrs(path, &fs.NodeAttrs{fs.NodeInfo{fs.TypeFifo, a.FileAttrs}, fs.DerivedAttrs{}})
}

func (w *StringWriter) Device(path string, a fs.DeviceAttrs) (err error) {
	return w.writeAttrs(path, &fs.NodeAttrs{fs.NodeInfo{fs.TypeDevice, a.FileAttrs}, fs.DerivedAttrs{}})
}

func (w *StringWriter) Dir(path, name string, a fs.FileAttrs) (err error) {
	return w.writeAttrs(name, &fs.NodeAttrs{fs.NodeInfo{fs.TypeDir, a}, fs.DerivedAttrs{}})
}

func (w *StringWriter) Mkdir(path string) (err error) {
	return w.writeEntry(path, "type="+string(fs.TypeDir))
}

func (w *StringWriter) Remove(path string) (err error) {
	return w.writeEntry(path, "type="+string(fs.TypeWhiteout))
}
