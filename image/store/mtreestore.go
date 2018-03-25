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

func (s *MtreeStore) Get(manifestDigest digest.Digest) (*mtree.DirectoryHierarchy, error) {
	file := s.mtreeFile(manifestDigest)
	f, err := os.Open(file)
	if err != nil {
		return nil, errors.Wrap(err, "read mtree")
	}
	defer f.Close()
	spec, err := mtree.ParseSpec(f)
	if err != nil {
		return nil, errors.Wrap(err, "read mtree")
	}
	return spec, nil
}

func (s *MtreeStore) Put(manifestDigest digest.Digest, spec *mtree.DirectoryHierarchy) error {
	destFile := s.mtreeFile(manifestDigest)
	if _, e := os.Stat(destFile); !os.IsNotExist(e) {
		// Cancel if already exists
		return nil
	}

	// Create mtree dir
	if err := os.MkdirAll(filepath.Dir(destFile), 0775); err != nil {
		return errors.Wrap(err, "create mtree dir")
	}

	// Create temp file
	tmpFile, err := ioutil.TempFile(filepath.Dir(destFile), ".tmp-mtree-")
	if err != nil {
		return errors.Wrap(err, "create mtree temp file")
	}
	defer tmpFile.Close()
	tmpName := tmpFile.Name()

	// Write mtree temp file
	if _, err = spec.WriteTo(io.Writer(tmpFile)); err != nil {
		return errors.Wrap(err, "write mtree temp file")
	}
	tmpFile.Close()

	// Rename mtree temp file
	if err = os.Rename(tmpName, destFile); err != nil {
		return errors.Wrap(err, "rename mtree temp file")
	}
	return nil
}

func (s *MtreeStore) Create(rootfs string, exclude []mtree.ExcludeFunc) (*mtree.DirectoryHierarchy, error) {
	dh, err := mtree.Walk(rootfs, exclude, mtreeKeywords, s.fsEval)
	if err != nil {
		return nil, errors.Wrap(err, "generate mtree spec")
	}
	return dh, nil
}

func (s *MtreeStore) Diff(from, to *mtree.DirectoryHierarchy) (diffs []mtree.InodeDelta, err error) {
	diffs, err = mtree.Compare(from, to, mtreeKeywords)
	if err != nil {
		err = errors.Wrap(err, "diff mtree")
	}
	return
}

func (s *MtreeStore) mtreeFile(manifestDigest digest.Digest) string {
	return filepath.Join(s.dir, manifestDigest.Algorithm().String(), manifestDigest.Hex())
}
