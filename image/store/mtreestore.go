package store

import (
	"os"

	"io"
	"io/ioutil"
	"path/filepath"

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

type MtreeStore struct {
	dir    string
	fsEval mtree.FsEval
}

func NewMtreeStore(dir string, fsEval mtree.FsEval) MtreeStore {
	return MtreeStore{dir, fsEval}
}

func (s *MtreeStore) Get(manifestDigest digest.Digest) (spec *mtree.DirectoryHierarchy, err error) {
	file := s.mtreeFile(manifestDigest)
	f, err := os.Open(file)
	if err == nil {
		defer f.Close()
		spec, err = mtree.ParseSpec(f)
	}
	if err != nil {
		err = errors.New("read mtree: " + err.Error())
	}
	return
}

func (s *MtreeStore) Put(manifestDigest digest.Digest, spec *mtree.DirectoryHierarchy) (err error) {
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
