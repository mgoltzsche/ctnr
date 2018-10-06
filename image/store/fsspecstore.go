package store

import (
	"io"
	"io/ioutil"

	"github.com/mgoltzsche/cntnr/pkg/fs"
	"github.com/mgoltzsche/cntnr/pkg/fs/tree"
	"github.com/mgoltzsche/cntnr/pkg/log"
	"github.com/opencontainers/go-digest"
	"github.com/pkg/errors"
)

type FsSpecStore struct {
	BlobStore
	debug log.Logger
}

func NewFsSpecStore(dir string, debug log.Logger) FsSpecStore {
	return FsSpecStore{NewBlobStore(dir), debug}
}

func (s *FsSpecStore) Get(fsId digest.Digest) (spec fs.FsNode, err error) {
	s.debug.Printf("Getting layer fsspec %s", fsId.Hex()[:13])
	r, err := s.BlobStore.Get(fsId)
	if err != nil {
		return nil, errors.Wrap(err, "fsspecstore")
	}
	b, err := ioutil.ReadAll(r)
	if err != nil {
		return nil, errors.Wrap(err, "fsspecstore")
	}
	spec, err = tree.ParseFsSpec(b)
	return spec, errors.Wrap(err, "fsspecstore: parse "+fsId.String())
}

func (s *FsSpecStore) Put(fsId digest.Digest, spec fs.FsNode) (err error) {
	s.debug.Printf("Storing layer fsspec %s", fsId.Hex()[:13])
	reader, writer := io.Pipe()
	defer func() {
		if e := reader.Close(); e != nil && err == nil {
			err = e
		}
	}()
	go func() (err error) {
		defer func() {
			writer.CloseWithError(errors.Wrap(err, "write fsspec"))
		}()
		return spec.WriteTo(writer, fs.AttrsAll)
	}()
	_, err = s.BlobStore.Put(fsId, reader)
	return errors.Wrap(err, "fsspecstore")
}
