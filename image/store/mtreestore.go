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

type MtreeStore struct {
	dir    string
	fsEval mtree.FsEval
	debug  log.Logger
}

func NewMtreeStore(dir string, fsEval mtree.FsEval, debug log.Logger) MtreeStore {
	return MtreeStore{dir, fsEval, debug}
}

func (s *MtreeStore) Get(manifestDigest digest.Digest) (spec *mtree.DirectoryHierarchy, err error) {
	file := s.mtreeFile(manifestDigest)
	f, err := os.Open(file)
	if err == nil {
		defer f.Close()
		spec, err = mtree.ParseSpec(f)
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
	s.debug.Printf("Storing layer mtree %s", manifestDigest)
	destFile := s.mtreeFile(manifestDigest)
	if _, err = os.Stat(destFile); !os.IsNotExist(err) {
		// Cancel if already exists
		return nil
	}

	// Create mtree dir
	if err = os.MkdirAll(filepath.Dir(destFile), 0775); err != nil {
		return errors.New("create mtree dir: " + err.Error())
	}

	// Write to temp file and rename
	tmpFile, err := ioutil.TempFile(filepath.Dir(destFile), ".tmp-mtree-")
	if err == nil {
		defer tmpFile.Close()
		tmpName := tmpFile.Name()
		if _, err = spec.WriteTo(io.Writer(tmpFile)); err == nil {
			tmpFile.Close()
			err = os.Rename(tmpName, destFile)
		}
	}
	if err != nil {
		err = errors.New("put mtree: " + err.Error())
	}
	return
}

func (s *MtreeStore) Create(rootfs string, exclude []mtree.ExcludeFunc) (dh *mtree.DirectoryHierarchy, err error) {
	partial := ""
	if exclude != nil {
		partial = " (partial)"
	}
	s.debug.Printf("Generating mtree of %s%s", rootfs, partial)
	if dh, err = mtree.Walk(rootfs, exclude, mtreeKeywords, s.fsEval); err != nil {
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

func (s *MtreeStore) mtreeFile(manifestDigest digest.Digest) string {
	return filepath.Join(s.dir, manifestDigest.Algorithm().String(), manifestDigest.Hex())
}
