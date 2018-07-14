package source

import (
	"compress/gzip"
	"os"

	"github.com/mgoltzsche/cntnr/pkg/fs"
	"github.com/pkg/errors"
)

var _ fs.Source = NewSourceTarGz("")

type sourceTarGz sourceTar

func NewSourceTarGz(file string) fs.Source {
	s := sourceTarGz(sourceTar{file, ""})
	return &s
}

func (s *sourceTarGz) Attrs() fs.NodeInfo {
	return (*sourceTar)(s).Attrs()
}

func (s *sourceTarGz) DeriveAttrs() (fs.DerivedAttrs, error) {
	return (*sourceTar)(s).DeriveAttrs()
}

func (s *sourceTarGz) Write(dest, name string, w fs.Writer, _ map[fs.Source]string) (err error) {
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
