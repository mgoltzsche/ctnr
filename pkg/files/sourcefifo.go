package files

var _ Source = &sourceFifo{}

type sourceFifo struct {
	attrs FileAttrs
}

func NewSourceFifo(attrs FileAttrs) Source {
	return &sourceFifo{attrs}
}

func (s *sourceFifo) Type() SourceType {
	return TypeFile
}

func (s *sourceFifo) Attrs() *FileAttrs {
	return &s.attrs
}

func (s *sourceFifo) Hash() (string, error) {
	return "", nil
}

func (s *sourceFifo) WriteFiles(dest string, w Writer) error {
	return w.Fifo(dest, s.attrs)
}
