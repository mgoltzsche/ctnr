package writer

import (
	"github.com/mgoltzsche/cntnr/pkg/fs"
	"github.com/mgoltzsche/cntnr/pkg/fs/source"
	"github.com/pkg/errors"
)

var _ fs.Writer = &FsNodeWriter{}

// Writer that builds a node tree and delegates operations to wrapped writer
// using the inserted node's path.
type FsNodeWriter struct {
	node fs.FsNode
	//writtenLower map[types.Source]string
	delegate fs.Writer
}

func NewFsNodeWriter(root fs.FsNode, delegate fs.Writer) (w *FsNodeWriter) {
	return &FsNodeWriter{root /*map[types.Source]string{},*/, delegate}
}

func (w *FsNodeWriter) FS() fs.FsNode {
	return w.node
}

func (w *FsNodeWriter) Parent() error {
	return w.delegate.Parent()
}

func (w *FsNodeWriter) Mkdir(dir string) (err error) {
	return w.delegate.Mkdir(dir)
}

func (w *FsNodeWriter) LowerNode(path, name string, a *fs.NodeAttrs) (err error) {
	node, err := w.node.AddLower(path, a)
	if err == nil {
		return
	}
	//w.writtenLower[node.source] = path
	return w.delegate.LowerNode(node.Path(), node.Name(), a)
}

func (w *FsNodeWriter) Lazy(path, name string, src fs.LazySource, written map[fs.Source]string) (err error) {
	node, err := w.node.AddUpper(path, src)
	if err == nil {
		return
	}
	return w.delegate.Lazy(node.Path(), node.Name(), src, written)
}

func (w *FsNodeWriter) File(file string, src fs.FileSource) (r fs.Source, err error) {
	node, err := w.node.AddUpper(file, src)
	if err != nil {
		return
	}
	r, err = w.delegate.File(node.Path(), src)
	node.SetSource(r)
	return
}

func (w *FsNodeWriter) Link(path, target string) error {
	linkedNode, targetNode, err := w.node.Link(path, target)
	if err != nil {
		return errors.WithMessage(err, "link")
	}
	return w.delegate.Link(linkedNode.Path(), targetNode.Path())
}

func (w *FsNodeWriter) LowerLink(path, target string, a *fs.NodeAttrs) error {
	node, err := w.node.AddLower(path, a)
	if err == nil {
		return errors.WithMessage(err, "lower link")
	}
	return w.delegate.LowerLink(node.Path(), target, a)
}

func (w *FsNodeWriter) Symlink(path string, a fs.FileAttrs) (err error) {
	node, err := w.node.AddUpper(path, source.NewSourceSymlink(a))
	if err != nil {
		return
	}
	return w.delegate.Symlink(node.Path(), a)
}

func (w *FsNodeWriter) Fifo(file string, a fs.DeviceAttrs) (err error) {
	node, err := w.node.AddUpper(file, source.NewSourceFifo(a))
	if err != nil {
		return
	}
	return w.delegate.Fifo(node.Path(), a)
}

func (w *FsNodeWriter) Device(path string, a fs.DeviceAttrs) (err error) {
	node, err := w.node.AddUpper(path, source.NewSourceDevice(a))
	if err != nil {
		return
	}
	return w.delegate.Device(node.Path(), a)
}

func (w *FsNodeWriter) Dir(dir, base string, a fs.FileAttrs) (err error) {
	node, err := w.node.AddUpper(dir, source.NewSourceDir(a))
	if err != nil {
		return
	}
	return w.delegate.Dir(node.Path(), node.Name(), a)
}

func (w *FsNodeWriter) Remove(file string) (err error) {
	node, err := w.node.AddWhiteout(file)
	if err != nil {
		return
	}
	return w.delegate.Remove(node.Path())
}
