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

func HashingNilWriter() Writer {
	return nilWriter
}

type hashingNilWriter string

func (_ hashingNilWriter) Parent() error { return nil }
func (_ hashingNilWriter) Lazy(path, name string, src LazySource, written map[Source]string) error {
	_, err := src.DeriveAttrs()
	return err
}
func (_ hashingNilWriter) File(path string, src FileSource) (Source, error) {
	_, err := src.DeriveAttrs()
	return src, err
}
func (_ hashingNilWriter) Link(path, target string) error                    { return nil }
func (_ hashingNilWriter) Symlink(path string, attrs FileAttrs) error        { return nil }
func (_ hashingNilWriter) Dir(path, name string, attrs FileAttrs) error      { return nil }
func (_ hashingNilWriter) Mkdir(path string) error                           { return nil }
func (_ hashingNilWriter) Fifo(path string, attrs DeviceAttrs) error         { return nil }
func (_ hashingNilWriter) Device(path string, attrs DeviceAttrs) error       { return nil }
func (_ hashingNilWriter) Remove(path string) error                          { return nil }
func (_ hashingNilWriter) LowerNode(path, name string, a *NodeAttrs) error   { return nil }
func (_ hashingNilWriter) LowerLink(path, target string, a *NodeAttrs) error { return nil }

// Writer that expands lazy resources
type ExpandingWriter struct {
	Writer
}

func (w *ExpandingWriter) Lazy(path, name string, src LazySource, written map[Source]string) (err error) {
	return src.Expand(path, w, written)
}
