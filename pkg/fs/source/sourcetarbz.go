package source

import (
	"compress/bzip2"
	"os"

	"github.com/mgoltzsche/cntnr/pkg/fs"
	"github.com/pkg/errors"
)

var _ fs.BlobSource = NewSourceTarBz("")

type sourceTarBz sourceTar

func NewSourceTarBz(file string) fs.BlobSource {
	s := sourceTarBz(sourceTar{file, ""})
	return &s
}

func (s *sourceTarBz) Equal(o fs.Source) (bool, error) {
	return (*sourceTar)(s).Equal(o)
}

func (s *sourceTarBz) Attrs() fs.NodeInfo {
	return (*sourceTar)(s).Attrs()
}

func (s *sourceTarBz) DerivedAttrs() (fs.NodeAttrs, error) {
	return (*sourceTar)(s).DerivedAttrs()
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
