package files

var _ Source = &sourceBlock{}

type sourceBlock struct {
	attrs FileAttrs
}

func NewSourceBlock(attrs FileAttrs) Source {
	return &sourceBlock{attrs}
}

func (s *sourceBlock) Type() SourceType {
	return TypeFile
}

func (s *sourceBlock) Attrs() *FileAttrs {
	return &s.attrs
}

func (s *sourceBlock) Hash() (string, error) {
	return "", nil
}

func (s *sourceBlock) WriteFiles(dest string, w Writer) error {
	return w.Block(dest, s.attrs)
}
