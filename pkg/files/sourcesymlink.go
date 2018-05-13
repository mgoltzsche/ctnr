package files

var _ Source = &sourceSymlink{}

type sourceSymlink struct {
	attrs FileAttrs
}

func NewSourceSymlink(attrs FileAttrs) Source {
	return &sourceSymlink{attrs}
}

func (s *sourceSymlink) Type() SourceType {
	return TypeSymlink
}

func (s *sourceSymlink) Attrs() *FileAttrs {
	return &s.attrs
}

func (s *sourceSymlink) Hash() (string, error) {
	return "", nil
}

func (s *sourceSymlink) WriteFiles(dest string, w Writer) error {
	return w.Symlink(dest, s.attrs)
}
