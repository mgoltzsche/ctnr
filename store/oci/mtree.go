package oci

import (
	"fmt"
	"os"

	"io"
	"io/ioutil"
	"path/filepath"

	"github.com/openSUSE/umoci/pkg/fseval"
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

func diffMtree(from, to *mtree.DirectoryHierarchy) (diffs []mtree.InodeDelta, err error) {
	diffs, err = mtree.Compare(from, to, mtreeKeywords)
	if err != nil {
		err = fmt.Errorf("diff mtree: %s", err)
	}
	return
}

func readMtree(file string) (*mtree.DirectoryHierarchy, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, fmt.Errorf("open mtree: %s", err)
	}
	defer f.Close()
	spec, err := mtree.ParseSpec(f)
	if err != nil {
		return nil, fmt.Errorf("parse mtree: %s", err)
	}
	return spec, nil
}

// Writes a mtree spec to disk for later diff
func writeMtree(spec *mtree.DirectoryHierarchy, destFile string) (err error) {
	// Create temp file
	tmpFile, err := ioutil.TempFile(filepath.Dir(destFile), "mtree-")
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

	return
}

func newMtree(rootfs string, fsEval mtree.FsEval) (*mtree.DirectoryHierarchy, error) {
	dh, err := mtree.Walk(rootfs, nil, mtreeKeywords, fsEval)
	if err != nil {
		return nil, fmt.Errorf("generate mtree spec: %s", err)
	}
	return dh, nil
}

func newFsEval(rootless bool) mtree.FsEval {
	fsEval := fseval.DefaultFsEval
	if rootless {
		fsEval = fseval.RootlessFsEval
	}
	return fsEval
}
