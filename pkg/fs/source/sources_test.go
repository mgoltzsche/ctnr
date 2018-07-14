package source

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mgoltzsche/cntnr/pkg/fs"
	"github.com/mgoltzsche/cntnr/pkg/fs/testutils"
	"github.com/mgoltzsche/cntnr/pkg/idutils"
	"github.com/openSUSE/umoci/pkg/fseval"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSources(t *testing.T) {
	tmpDir, rootfs := createTestFileSystem(t)
	defer os.RemoveAll(tmpDir)

	// Prepare source files
	tarFile := filepath.Join(tmpDir, "archive.tar")
	tarGzFile := filepath.Join(tmpDir, "archive.tar.gz")
	tarBzFile := filepath.Join(tmpDir, "archive.tar.bz")
	tarDir(t, rootfs, "-cf", tarFile)
	tarDir(t, rootfs, "-czf", tarGzFile)
	tarDir(t, rootfs, "-cjf", tarBzFile)
	srcFile := filepath.Join(tmpDir, "sourcefile")
	srcLink := filepath.Join(tmpDir, "sourcelink")
	err := ioutil.WriteFile(srcFile, []byte("content"), 0644)
	require.NoError(t, err)
	err = os.Symlink(srcFile, srcLink)
	require.NoError(t, err)
	mtime, err := time.Parse(time.RFC3339, "2018-01-23T01:01:42Z")
	require.NoError(t, err)
	atime, err := time.Parse(time.RFC3339, "2018-01-23T01:02:42Z")
	require.NoError(t, err)

	// Test source file overlay variants
	testee := NewSources(fseval.RootlessFsEval, idutils.MapRootless)
	factory := func(file string, fi os.FileInfo, usr *idutils.UserIds) (fs.Source, error) {
		return testee.File(file, fi, usr)
	}
	factoryX := func(file string, fi os.FileInfo, usr *idutils.UserIds) (fs.Source, error) {
		return testee.FileOverlay(file, fi, usr)
	}
	expectedArchivePaths := toWrittenPathMap(expectedWrittenPaths("/overlayx"))
	expectedArchivePaths["/"] = true
	expectedFilePaths := map[string]bool{"/": true, "/overlayx": true}
	for _, c := range []struct {
		t             fs.NodeType
		factory       func(file string, fi os.FileInfo, usr *idutils.UserIds) (fs.Source, error)
		file          string
		expectedPaths map[string]bool
	}{
		{fs.TypeFile, factory, srcFile, expectedFilePaths},
		{fs.TypeFile, factoryX, srcFile, expectedFilePaths},
		{fs.TypeSymlink, factory, srcLink, expectedFilePaths},
		{fs.TypeSymlink, factoryX, srcLink, expectedFilePaths},
		{fs.TypeOverlay, factoryX, tarFile, expectedArchivePaths},
		{fs.TypeOverlay, factoryX, tarGzFile, expectedArchivePaths},
		{fs.TypeOverlay, factoryX, tarBzFile, expectedArchivePaths},
		// TODO: test device and URL
	} {
		writerMock := testutils.NewWriterMock(t, fs.AttrsAll)
		fi, err := os.Lstat(c.file)
		require.NoError(t, err)
		src, err := c.factory(c.file, fi, nil)
		require.NoError(t, err)
		a := src.Attrs()
		st := a.NodeType
		if st != c.t {
			t.Errorf("%s: expected type %s but was %s", c.file, c.t, st)
			t.FailNow()
		}

		// Test hash
		wa, err := src.DeriveAttrs()
		require.NoError(t, err)
		hash1 := wa.Hash

		src, err = c.factory(c.file, fi, &idutils.UserIds{99997, 99997})
		require.NoError(t, err)
		wa, err = src.DeriveAttrs()
		require.NoError(t, err)
		hash2 := wa.Hash
		if hash2 == "" && c.t != fs.TypeSymlink {
			t.Errorf("%s: source hash is empty", c.file)
		}
		if hash1 != hash2 {
			t.Errorf("%s: hash1 != hash1", c.file)
		}

		// Test write
		writerMock.Dir("/", "", fs.FileAttrs{Mode: 0755})
		err = src.Write("/overlayx", "", &testutils.ExpandingWriterMock{writerMock}, map[fs.Source]string{})
		require.NoError(t, err)
		if !assert.Equal(t, c.expectedPaths, writerMock.WrittenPaths, c.file) {
			t.FailNow()
		}
	}

	// Test if actual file attributes applied
	testee = NewSources(fseval.RootlessFsEval, idutils.MapIdentity)
	err = fseval.RootlessFsEval.Lutimes(srcFile, atime, mtime)
	require.NoError(t, err)
	uid := os.Geteuid()
	gid := os.Getegid()
	writerMock := testutils.NewWriterMock(t, fs.AttrsAll)
	fi, err := os.Lstat(srcFile)
	require.NoError(t, err)
	r, err := testee.File(srcFile, fi, nil)
	require.NoError(t, err)
	err = r.Write("/file1", "file1", writerMock, map[fs.Source]string{})
	require.NoError(t, err)
	usr := ""
	if uid != 0 || gid != 0 {
		usr = fmt.Sprintf(" usr=%d:%d", uid, gid)
	}
	expected := fmt.Sprintf("/file1 type=file%s mode=644 size=7 mtime=1516669302", usr)
	expected += " atime=1516669362 hash=sha256:ed7002b439e9ac845f22357d822bac1444730fbdb6016d3ec9432297b9ec9f73"
	assert.Equal(t, []string{expected}, writerMock.Written, "sources should map file attributes")
}

func toWrittenPathMap(paths []string) map[string]bool {
	r := map[string]bool{}
	for _, line := range paths {
		r[strings.Split(line, " ")[0]] = true
	}
	return r
}
