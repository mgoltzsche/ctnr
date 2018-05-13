package files

var _ Source = &sourceDir{}

type sourceDir struct {
	attrs FileAttrs
}

func NewSourceDir(attrs FileAttrs) Source {
	return &sourceDir{attrs}
}

func (s *sourceDir) Type() SourceType {
	return TypeDir
}

func (s *sourceDir) Attrs() *FileAttrs {
	return &s.attrs
}

func (s *sourceDir) Hash() (d string, err error) {
	return "", nil
}

func (s *sourceDir) WriteFiles(dest string, w Writer) error {
	return w.Dir(dest, s.attrs)
}

type sourceDirImplicit struct {
	sourceDir
}

func (s *sourceDirImplicit) WriteFiles(dest string, w Writer) error {
	return w.DirImplicit(dest, s.attrs)
}
