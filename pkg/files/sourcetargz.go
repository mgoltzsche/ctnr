package files

import (
	"compress/gzip"
	"os"

	"github.com/pkg/errors"
)

var _ Source = NewSourceTarGz("", FileAttrs{})

type sourceTarGz sourceTar

func NewSourceTarGz(file string, attrs FileAttrs) Source {
	s := sourceTarGz(sourceTar{file, attrs, ""})
	return &s
}

func (s *sourceTarGz) Type() SourceType {
	return (*sourceTar)(s).Type()
}

func (s *sourceTarGz) Attrs() *FileAttrs {
	return (*sourceTar)(s).Attrs()
}

func (s *sourceTarGz) Hash() (h string, err error) {
	if h, err = (*sourceTar)(s).Hash(); err == nil {
		h = "gz:" + h
	}
	return
}

func (s *sourceTarGz) WriteFiles(dest string, w Writer) (err error) {
	f, err := os.Open(s.file)
	if err != nil {
		return errors.Wrap(err, "extract tar.gz")
	}
	defer f.Close()
	r, err := gzip.NewReader(f)
	if err != nil {
		return errors.Wrap(err, "extract tar.gz")
	}
	if err = unpackTar(r, dest, w); err != nil {
		return errors.Wrap(err, "extract tar.gz")
	}
	return
}
