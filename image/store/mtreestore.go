package store

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/mgoltzsche/cntnr/pkg/log"
	digest "github.com/opencontainers/go-digest"
	"github.com/pkg/errors"
	"github.com/vbatts/go-mtree"
)

var mtreeKeywords = []mtree.Keyword{
	"size",
	"type",
	"uid",
	"gid",
	"mode",
	"link",
	"nlink",
	"tar_time",
	"sha256digest",
	"xattr",
}

type notExistError struct {
	cause error
}

func (e *notExistError) Error() string {
	return e.cause.Error()
}

func (e *notExistError) Format(s fmt.State, verb rune) {
	type formatter interface {
		Format(s fmt.State, verb rune)
	}
	e.cause.(formatter).Format(s, verb)
}

func IsNotExist(err error) bool {
	_, ok := err.(*notExistError)
	return ok
}

// TODO: RetainAll(manifestDigests)
type MtreeStore struct {
	dir    string
	fsEval mtree.FsEval
	debug  log.Logger
}

func NewMtreeStore(dir string, fsEval mtree.FsEval, debug log.Logger) MtreeStore {
	return MtreeStore{dir, fsEval, debug}
}

func (s *MtreeStore) Exists(manifestDigest digest.Digest) (bool, error) {
	link := s.linkFile(manifestDigest)
	if _, e := os.Stat(link); e != nil {
		if os.IsNotExist(e) {
			return false, nil
		} else {
			return false, errors.New("mtree: " + e.Error())
		}
	}
	return true, nil
}

func (s *MtreeStore) Get(manifestDigest digest.Digest) (spec *mtree.DirectoryHierarchy, err error) {
	s.debug.Printf("Getting layer mtree %s", manifestDigest.Hex()[:13])

	link := s.linkFile(manifestDigest)
	file, err := os.Readlink(link)
	if err == nil {
		file = filepath.Join(filepath.Dir(link), file)
		var f *os.File
		if f, err = os.Open(file); err == nil {
			defer f.Close()
			spec, err = mtree.ParseSpec(f)
		}
	}
	if err != nil {
		if os.IsNotExist(err) {
			err = &notExistError{errors.Errorf("mtree %s does not exist", manifestDigest)}
		} else {
			err = errors.New("read mtree: " + err.Error())
		}
	}
	return
}

func (s *MtreeStore) Put(manifestDigest digest.Digest, spec *mtree.DirectoryHierarchy) (err error) {
	s.debug.Printf("Storing layer mtree %s", manifestDigest.Hex()[:13])

	linkFile := s.linkFile(manifestDigest)

	if _, err = os.Lstat(linkFile); err == nil || !os.IsNotExist(err) {
		if err != nil {
			err = errors.New("mtree: " + err.Error())
		}
		return
	}

	if err = os.MkdirAll(filepath.Dir(linkFile), 0775); err != nil {
		return errors.New("mtree dir: " + err.Error())
	}

	// Write temp file
	f, err := ioutil.TempFile(s.dir, ".tmp-mtree-")
	if err != nil {
		return errors.New("mtree temp file: " + err.Error())
	}
	tmpFile := f.Name()
	renamed := false
	defer func() {
		f.Close()
		if !renamed {
			os.Remove(tmpFile)
		}
	}()
	digester := digest.SHA256.Digester()
	w := normWriter(io.MultiWriter(f, digester.Hash()))
	if _, err = spec.WriteTo(w); err != nil {
		return errors.New("mtree: " + err.Error())
	}

	mtreeDigest := digester.Digest()
	mtreeFile := s.mtreeFile(mtreeDigest)

	// Move to final mtree file if not exists
	if _, err = os.Stat(mtreeFile); os.IsNotExist(err) {
		if err = f.Sync(); err == nil {
			if err = f.Close(); err == nil {
				if err = os.MkdirAll(filepath.Dir(mtreeFile), 0775); err == nil {
					if err = os.Rename(tmpFile, mtreeFile); err == nil {
						renamed = true
					}
				}
			}
		}
	}

	// Link manifest digest to mtree file
	if err == nil {
		err = os.Symlink(filepath.Join("..", "..", "mtree", mtreeDigest.Algorithm().String(), mtreeDigest.Hex()), linkFile)
	}

	if err != nil {
		err = errors.New("mtree: " + err.Error())
	}

	return
}

func (s *MtreeStore) Create(rootfs string) (dh *mtree.DirectoryHierarchy, err error) {
	s.debug.Printf("Generating mtree of %s", rootfs)
	if dh, err = mtree.Walk(rootfs, nil, mtreeKeywords, s.fsEval); err != nil {
		err = errors.New("generate mtree spec: " + err.Error())
	}
	return
}

func (s *MtreeStore) Diff(from, to *mtree.DirectoryHierarchy) (diffs []mtree.InodeDelta, err error) {
	if diffs, err = mtree.Compare(from, to, mtreeKeywords); err != nil {
		err = errors.New("diff mtree: " + err.Error())
	}
	return
}

func (s *MtreeStore) linkFile(manifestDigest digest.Digest) string {
	return filepath.Join(s.dir, "ref", manifestDigest.Algorithm().String(), manifestDigest.Hex())
}

func (s *MtreeStore) mtreeFile(mtreeDigest digest.Digest) string {
	return filepath.Join(s.dir, "mtree", mtreeDigest.Algorithm().String(), mtreeDigest.Hex())
}
