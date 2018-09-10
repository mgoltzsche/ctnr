package store

import (
	"io/ioutil"
	"os"
	"path/filepath"

	exterrors "github.com/mgoltzsche/cntnr/pkg/errors"
	"github.com/mgoltzsche/cntnr/pkg/fs"
	"github.com/mgoltzsche/cntnr/pkg/fs/tree"
	"github.com/mgoltzsche/cntnr/pkg/log"
	digest "github.com/opencontainers/go-digest"
	"github.com/pkg/errors"
)

const errFsSpecNotExist = "github.com/mgoltzsche/cntnr/image/fsspec/notexist"

func IsFsSpecNotExist(err error) bool {
	return exterrors.HasType(err, errFsSpecNotExist)
}

// TODO: RetainAll(fsIds)
type FsSpecStore struct {
	dir   string
	debug log.Logger
}

func NewFsSpecStore(dir string, debug log.Logger) FsSpecStore {
	return FsSpecStore{dir, debug}
}

func (s *FsSpecStore) Exists(fsId digest.Digest) (bool, error) {
	if _, e := os.Stat(s.specFile(fsId)); e != nil {
		if os.IsNotExist(e) {
			return false, nil
		} else {
			return false, errors.New("fsspec: " + e.Error())
		}
	}
	return true, nil
}

func (s *FsSpecStore) Get(fsId digest.Digest) (spec fs.FsNode, err error) {
	s.debug.Printf("Getting layer fs spec %s", fsId.Hex()[:13])

	file := s.specFile(fsId)
	b, err := ioutil.ReadFile(file)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, exterrors.Typedf(errFsSpecNotExist, "fsspec %s does not exist", fsId)
		} else {
			return nil, errors.New("read fsspec: " + err.Error())
		}
	}

	spec, err = tree.ParseFsSpec(b)
	err = errors.Wrap(err, "parse fsspec "+file)
	return
}

func (s *FsSpecStore) Put(fsId digest.Digest, spec fs.FsNode) (err error) {
	s.debug.Printf("Storing layer fs spec %s", fsId.Hex()[:13])

	destFile := s.specFile(fsId)

	if err = os.MkdirAll(filepath.Dir(destFile), 0775); err != nil {
		return errors.New("fsspec dir: " + err.Error())
	}

	// Write temp file
	f, err := ioutil.TempFile(filepath.Dir(destFile), ".tmp-fsspec-")
	if err != nil {
		return errors.New("store fsspec: " + err.Error())
	}
	tmpFile := f.Name()
	renamed := false
	defer func() {
		f.Close()
		if !renamed {
			os.Remove(tmpFile)
		}
	}()
	if err = spec.WriteTo(f, fs.AttrsAll); err != nil {
		return errors.Wrap(err, "store fsspec")
	}

	// Move to final spec file if not exists
	if err = f.Sync(); err == nil {
		if err = f.Close(); err == nil {
			if err = os.Rename(tmpFile, destFile); err == nil {
				renamed = true
			}
		}
	}
	if err != nil {
		err = errors.New("store fsspec: " + err.Error())
	}

	return
}

func (s *FsSpecStore) specFile(fsId digest.Digest) string {
	return filepath.Join(s.dir, fsId.Algorithm().String(), fsId.Hex())
}
