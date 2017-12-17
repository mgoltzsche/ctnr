package store

import (
	"fmt"
	"os"

	"io"
	"io/ioutil"
	"path/filepath"

	digest "github.com/opencontainers/go-digest"
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
		return nil, fmt.Errorf("read mtree: %s", err)
	}
	defer f.Close()
	spec, err := mtree.ParseSpec(f)
	if err != nil {
		return nil, fmt.Errorf("read mtree: %s", err)
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
		return fmt.Errorf("create mtree dir: %s", err)
	}

	// Create temp file
	tmpFile, err := ioutil.TempFile(filepath.Dir(destFile), "tmpmtree-")
	if err != nil {
		return fmt.Errorf("create mtree temp file: %s", err)
	}
	defer tmpFile.Close()
	tmpName := tmpFile.Name()

	// Write mtree temp file
	if _, err = spec.WriteTo(io.Writer(tmpFile)); err != nil {
		return fmt.Errorf("write mtree temp file: %s", err)
	}
	tmpFile.Close()

	// Rename mtree temp file
	if err = os.Rename(tmpName, destFile); err != nil {
		return fmt.Errorf("rename mtree temp file: %s", err)
	}
	return nil
}

func (s *MtreeStore) Create(rootfs string) (*mtree.DirectoryHierarchy, error) {
	dh, err := mtree.Walk(rootfs, nil, mtreeKeywords, s.fsEval)
	if err != nil {
		return nil, fmt.Errorf("generate mtree spec: %s", err)
	}
	return dh, nil
}

func (s *MtreeStore) Diff(from, to *mtree.DirectoryHierarchy) (diffs []mtree.InodeDelta, err error) {
	diffs, err = mtree.Compare(from, to, mtreeKeywords)
	if err != nil {
		err = fmt.Errorf("diff mtree: %s", err)
	}
	return
}

func (s *MtreeStore) mtreeFile(manifestDigest digest.Digest) string {
	return filepath.Join(s.dir, manifestDigest.Algorithm().String(), manifestDigest.Hex())
}
