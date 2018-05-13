package files

import (
	"compress/bzip2"
	"os"

	"github.com/pkg/errors"
)

var _ Source = NewSourceTarBz("", FileAttrs{})

type sourceTarBz sourceTar

func NewSourceTarBz(file string, attrs FileAttrs) Source {
	s := sourceTarBz(sourceTar{file, attrs, ""})
	return &s
}

func (s *sourceTarBz) Type() SourceType {
	return (*sourceTar)(s).Type()
}

func (s *sourceTarBz) Attrs() *FileAttrs {
	return (*sourceTar)(s).Attrs()
}

func (s *sourceTarBz) Hash() (h string, err error) {
	if h, err = (*sourceTar)(s).Hash(); err == nil {
		h = "bz:" + h
	}
	return
}

func (s *sourceTarBz) WriteFiles(dest string, w Writer) (err error) {
	f, err := os.Open(s.file)
	if err != nil {
		return errors.Wrap(err, "extract tar.bz")
	}
	defer f.Close()
	r := bzip2.NewReader(f)
	if err = unpackTar(r, dest, w); err != nil {
		return errors.Wrap(err, "extract tar.bz")
	}
	return
}
