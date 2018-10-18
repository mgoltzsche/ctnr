package source

import (
	"compress/bzip2"
	"os"

	"github.com/mgoltzsche/ctnr/pkg/fs"
	"github.com/pkg/errors"
)

var _ fs.Source = NewSourceTarBz("")

type sourceTarBz sourceTar

func NewSourceTarBz(file string) fs.Source {
	s := sourceTarBz(sourceTar{file, ""})
	return &s
}

func (s *sourceTarBz) Attrs() fs.NodeInfo {
	return (*sourceTar)(s).Attrs()
}

func (s *sourceTarBz) DeriveAttrs() (fs.DerivedAttrs, error) {
	return (*sourceTar)(s).DeriveAttrs()
}

func (s *sourceTarBz) Write(dest, name string, w fs.Writer, _ map[fs.Source]string) (err error) {
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
