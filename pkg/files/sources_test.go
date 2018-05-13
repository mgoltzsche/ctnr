package files

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mgoltzsche/cntnr/pkg/idutils"
	"github.com/openSUSE/umoci/pkg/fseval"
	"github.com/stretchr/testify/require"
	"github.com/vbatts/go-mtree"
)

func TestSources(t *testing.T) {
	tmpDir, rootfs := createTestFileSystem(t)
	defer os.RemoveAll(tmpDir)

	// Prepare source files
	assertFsState(t, rootfs, "", expectedTestfsState)
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

	// Test source file overlay variants
	testee := NewSources(fseval.RootlessFsEval, idutils.MapRootless)
	factory := func(file string, fi os.FileInfo, usr *idutils.UserIds) (Source, error) {
		return testee.File(file, fi, usr)
	}
	factoryX := func(file string, fi os.FileInfo, usr *idutils.UserIds) (Source, error) {
		return testee.FileOverlay(file, fi, usr)
	}
	expectedArchivePaths := mtreeToExpectedPaths(t, "/overlayx", expectedTestfsState)
	writerMock := newWriterMock(t)
	for _, c := range []struct {
		t             SourceType
		factory       func(file string, fi os.FileInfo, usr *idutils.UserIds) (Source, error)
		file          string
		expectedPaths map[string]bool
	}{
		{TypeFile, factory, srcFile, map[string]bool{"/": true, "/overlayx": true}},
		{TypeFile, factoryX, srcFile, map[string]bool{"/": true, "/overlayx": true}},
		{TypeSymlink, factory, srcLink, map[string]bool{"/": true, "/overlayx": true}},
		{TypeSymlink, factoryX, srcLink, map[string]bool{"/": true, "/overlayx": true}},
		{TypeOverlay, factoryX, tarFile, expectedArchivePaths},
		{TypeOverlay, factoryX, tarGzFile, expectedArchivePaths},
		{TypeOverlay, factoryX, tarBzFile, expectedArchivePaths},
	} {
		fi, err := os.Lstat(c.file)
		require.NoError(t, err)
		src, err := c.factory(c.file, fi, nil)
		require.NoError(t, err)
		st := src.Type()
		if st != c.t {
			t.Errorf("%s: src.Type(): expected %s but was %s", c.file, c.t, st)
		}
		a := src.Attrs()
		if a == nil {
			t.Error("src.Attrs() returned nil")
			t.FailNow()
		}

		// Test hash
		hash1, err := src.Hash()
		require.NoError(t, err)
		src, err = c.factory(c.file, fi, &idutils.UserIds{99997, 99997})
		require.NoError(t, err)
		hash2, err := src.Hash()
		require.NoError(t, err)
		if hash2 == "" && c.t != TypeSymlink {
			t.Errorf("%s: source hash is empty", c.file)
		}
		if hash1 != hash2 {
			t.Errorf("%s: hash1 != hash1", c.file)
		}

		// Test write
		writerMock.DirImplicit("", FileAttrs{Mode: 0755})
		err = src.WriteFiles("/overlayx", writerMock)
		require.NoError(t, err)
		assertPathsWritten(t, c.expectedPaths, writerMock.writtenPaths, c.file)
	}
}

func assertPathsWritten(t *testing.T, expected, actual map[string]bool, msg string) {
	for a, _ := range actual {
		if !expected[a] {
			t.Errorf("%s: Unexpected path written: %s", msg, a)
		}
	}
	for e, _ := range expected {
		if !actual[e] {
			t.Errorf("%s: Missing path: %s", msg, e)
		}
	}
}

func mtreeToExpectedPaths(t *testing.T, rootPath, dhStr string) map[string]bool {
	r := map[string]bool{"/": true}
	dhStr = strings.Replace(dhStr, "$ROOT", rootPath, -1)
	dh, err := mtree.ParseSpec(strings.NewReader(dhStr))
	require.NoError(t, err)
	diff, err := mtree.Compare(&mtree.DirectoryHierarchy{}, dh, mtreeTestkeywords)
	require.NoError(t, err)
	for _, e := range diff {
		r[filepath.Join(rootPath, e.Path())] = true
	}
	return r
}
