package fs

type Writer interface {
	Lazy(path, name string, src LazySource, written map[Source]string) error
	File(path string, src FileSource) (Source, error)
	Link(path, target string) error
	Symlink(path string, attrs FileAttrs) error
	Dir(path, base string, attrs FileAttrs) error
	Mkdir(path string) error
	Fifo(path string, attrs DeviceAttrs) error
	Device(path string, attrs DeviceAttrs) error
	Remove(path string) error
	LowerNode(path, name string, a *NodeAttrs) error
	LowerLink(path, target string, a *NodeAttrs) error
	Parent() error
}

func NoopWriter() Writer {
	return nilWriter
}

type noopWriter string

func (_ noopWriter) Parent() error { return nil }
func (_ noopWriter) Lazy(path, name string, src LazySource, written map[Source]string) error {
	return nil
}
func (_ noopWriter) File(path string, src FileSource) (Source, error)  { return src, nil }
func (_ noopWriter) Link(path, target string) error                    { return nil }
func (_ noopWriter) Symlink(path string, attrs FileAttrs) error        { return nil }
func (_ noopWriter) Dir(path, name string, attrs FileAttrs) error      { return nil }
func (_ noopWriter) Mkdir(path string) error                           { return nil }
func (_ noopWriter) Fifo(path string, attrs DeviceAttrs) error         { return nil }
func (_ noopWriter) Device(path string, attrs DeviceAttrs) error       { return nil }
func (_ noopWriter) Remove(path string) error                          { return nil }
func (_ noopWriter) LowerNode(path, name string, a *NodeAttrs) error   { return nil }
func (_ noopWriter) LowerLink(path, target string, a *NodeAttrs) error { return nil }

// Writer that expands lazy resources
type ExpandingWriter struct {
	Writer
}

func (w *ExpandingWriter) Lazy(path, name string, src LazySource, written map[Source]string) (err error) {
	return src.Expand(path, w, written)
}
