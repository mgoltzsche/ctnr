package tree

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mgoltzsche/cntnr/pkg/fs"
	"github.com/mgoltzsche/cntnr/pkg/fs/source"
	"github.com/mgoltzsche/cntnr/pkg/fs/testutils"
	"github.com/mgoltzsche/cntnr/pkg/fs/writer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vbatts/go-mtree"
)

func absDirs(baseDir string, file []string) []string {
	files := make([]string, len(file))
	for i, f := range file {
		files[i] = filepath.Join(baseDir, f)
	}
	return files
}

func TestFsBuilder(t *testing.T) {
	tmpDir, err := ioutil.TempDir("", "fsbuilder-test-")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)
	rootfs := filepath.Join(tmpDir, "rootfs")
	opts := fs.NewFSOptions(true)
	warn := log.New(os.Stdout, "warn: ", 0)
	testutils.WriteTestFileSystem(t, writer.NewDirWriter(rootfs, opts, warn))

	//
	// FILE SYSTEM TREE CONSTRUCTION TESTS
	//

	// Test AddAll() - source creation and mapping to destination path
	expectedFileA := map[string]bool{"/": true, "/dir": true, "/dir/fileA": true}
	expectedFilesAB := map[string]bool{
		"/":          true,
		"/dir":       true,
		"/dir/fileA": true,
		"/dir/fileB": true,
	}
	expectedFileCopyOps := map[string]bool{
		"/":                    true,
		"/dir":                 true,
		"/dir/x":               true,
		"/dir/x/fifo":          true,
		"/dir/x/fileA":         true,
		"/dir/x/link-abs":      true,
		"/dir/x/nesteddir":     true,
		"/dir/x/nestedsymlink": true,
		"/dir/x/symlink-abs":   true,
	}
	expectedDir1CopyOps := map[string]bool{
		"/":                            true,
		"/dest":                        true,
		"/dest/dir":                    true,
		"/dest/dir/file1":              true,
		"/dest/dir/file2":              true,
		"/dest/dir/sdir":               true,
		"/dest/dir/sdir/nesteddir":     true,
		"/dest/dir/sdir/nestedsymlink": true,
	}
	expectedRootfsOps := mtreeToExpectedPathSet(t, "/all", testutils.ExpectedTestfsState)
	for _, c := range []struct {
		src           []string
		dest          string
		expand        bool
		expectedPaths map[string]bool
	}{
		{[]string{"rootfs/etc/fileA", "rootfs/etc/link-abs", "rootfs/etc/symlink-abs", "rootfs/dir1/sdir", "rootfs/etc/fifo"}, "dir/x", false, expectedFileCopyOps},
		{[]string{"rootfs/etc/fileA", "rootfs/etc/link-abs", "rootfs/etc/symlink-abs", "rootfs/dir1/sdir", "rootfs/etc/fifo"}, "dir/x/", false, expectedFileCopyOps},
		{[]string{"rootfs/etc/fileA"}, "dir/fileX", false, map[string]bool{"/": true, "/dir": true, "/dir/fileX": true}},
		{[]string{"rootfs/etc/fileA"}, "dir/", false, expectedFileA},
		{[]string{filepath.Join(tmpDir, "rootfs/etc/fileA")}, "dir/", false, expectedFileA},
		{[]string{"rootfs/etc/file*"}, "dir", false, expectedFilesAB},
		{[]string{"rootfs/dir1"}, "dest/dir", false, expectedDir1CopyOps},
		{[]string{"rootfs/dir1/"}, "dest/dir", false, expectedDir1CopyOps},
		{[]string{"rootfs/dir1"}, "dest/dir/", false, expectedDir1CopyOps},
		{[]string{"rootfs/dir1/"}, "dest/dir/", false, expectedDir1CopyOps},
		{[]string{"rootfs"}, "/all/", false, expectedRootfsOps},
		// TODO: add URL source test case
	} {
		rootfs := newFS()
		testee := NewFsBuilder(rootfs, opts)
		testee.AddAll(tmpDir, c.src, c.dest, nil)
		w := testutils.NewWriterMock(t, fs.AttrsAll)
		err := testee.Write(w)
		require.NoError(t, err)
		rootfs.MockDevices()
		// need to assert against path map since archive content write order is not guaranteed
		if !assert.Equal(t, c.expectedPaths, w.WrittenPaths, fmt.Sprintf("AddAll(ctx, %+v, %s): unexpected written paths", c.src, c.dest)) {
			t.FailNow()
		}
		_, err = testee.Hash(fs.AttrsAll)
		require.NoError(t, err)
	}

	// Test error
	testee := NewFsBuilder(newFS(), opts)
	testee.AddAll(tmpDir, []string{"not-existing"}, "/", nil)
	err = testee.Write(fs.HashingNilWriter())
	require.Error(t, err, "using not existing file as source should yield error")

	//
	// CONSISTENCY TESTS
	//

	// Test written node tree equals original
	testee = NewFsBuilder(newFS(), opts)
	testee.AddDir(rootfs, "/", nil)
	var buf bytes.Buffer
	tree, err := testee.FS()
	require.NoError(t, err)
	err = tree.WriteTo(&buf, fs.AttrsCompare)
	require.NoError(t, err)
	expectedStr := buf.String()
	expectedWritten := testutils.MockWrites(t, tree).Written
	expectedWritten2 := testutils.MockWrites(t, tree).Written
	if !assert.Equal(t, expectedWritten, expectedWritten2, "Write() should be idempotent") {
		t.FailNow()
	}
	// Create tar
	tarFile := filepath.Join(tmpDir, "archive.tar")
	f, err := os.OpenFile(tarFile, os.O_CREATE|os.O_RDWR, 0640)
	require.NoError(t, err)
	defer f.Close()
	tw := writer.NewTarWriter(f)
	err = testee.Write(tw)
	require.NoError(t, err)
	err = tw.Close()
	require.NoError(t, err)
	f.Close()

	// Read, extract and compare cases
	for i, c := range []string{"rootfs", "archive.tar"} {
		testee := NewFsBuilder(newFS(), opts)
		if i == 0 {
			// rootfs dir
			testee.AddDir(filepath.Join(tmpDir, c), "/", nil)
		} else {
			// archive
			testee.AddAll(tmpDir, []string{c}, "/", nil)
		}
		// Normalize
		rootfs := filepath.Join(tmpDir, "rootfs"+fmt.Sprintf("%d", i))
		dirWriter := writer.NewDirWriter(rootfs, opts, warn)
		nodeWriter := writer.NewFsNodeWriter(newFS(), dirWriter)
		err = testee.Write(&fs.ExpandingWriter{nodeWriter})
		require.NoError(t, err)
		err = dirWriter.Close()
		require.NoError(t, err)
		// Assert normalized string representation equals original
		nodes := nodeWriter.FS()
		buf.Reset()
		err = nodes.WriteTo(&buf, fs.AttrsCompare)
		require.NoError(t, err)
		if !assert.Equal(t, strings.Split(expectedStr, "\n"), strings.Split(buf.String(), "\n"), "string(expand("+c+")) != string(sourcedir{"+c+"})") {
			t.FailNow()
		}
		// Write nodes written by FsNodeWriter should equal original
		if !assert.Equal(t, expectedWritten, testutils.MockWrites(t, nodes).Written, "a.Write(nodeWriter); nodeWriter.Write() should write same as original") {
			t.FailNow()
		}
		// Assert fs.Diff(fs) should return empty tree
		diff, err := tree.Diff(nodes.(*FsNode))
		require.NoError(t, err)
		if !assert.Equal(t, []string{}, testutils.MockWrites(t, diff).Written, "a.Diff(a) should be empty") {
			t.FailNow()
		}
		// Assert fs.Diff(changedFs) == change
		_, err = nodes.AddUpper("/etc/dir1", source.NewSourceDir(fs.FileAttrs{Mode: os.ModeDir | 0740}))
		require.NoError(t, err)
		diff, err = tree.Diff(nodes.(*FsNode))
		require.NoError(t, err)
		w := testutils.NewWriterMock(t, fs.AttrsHash)
		err = diff.Write(w)
		expectedOps := []string{
			"/ type=dir mode=775",
			"/etc type=dir mode=775",
			"/etc/dir1 type=dir mode=740",
		}
		if !assert.Equal(t, expectedOps, w.Written, "fs.Diff(changedFs) == changes") {
			t.FailNow()
		}
	}

	// Test Hash()
	testee = NewFsBuilder(newFS(), opts)
	testee.AddFiles(filepath.Join(rootfs, "etc/fileA"), "fileA", nil)
	hash1, err := testee.Hash(fs.AttrsHash)
	require.NoError(t, err)
	testee = NewFsBuilder(newFS(), opts)
	testee.AddFiles(filepath.Join(rootfs, "etc/fileA"), "fileA", nil)
	hash2, err := testee.Hash(fs.AttrsHash)
	require.NoError(t, err)
	if hash1 != hash2 {
		t.Errorf("Hash(): should be idempotent")
		t.FailNow()
	}
	testee.AddFiles(filepath.Join(rootfs, "etc/fileB"), "fileA", nil)
	hash2, err = testee.Hash(fs.AttrsCompare)
	require.NoError(t, err)
	if hash1 == hash2 {
		t.Errorf("Hash(): must return changed value when content changed")
		t.FailNow()
	}
}

// TODO: enable again
/*func TestFileSystemBuilderRootfsBoundsViolation(t *testing.T) {
	for _, c := range []struct {
		src  string
		dest string
		msg  string
	}{
		{"/dir2", "../outsiderootfs", "destination outside rootfs directory was not rejected"},
		{"dir1/sdir1/linkInvalid", "/dirx", "linking outside rootfs directory was not rejected"},
		//{"/dir2"}, "/dirx", "source path outside context directory was not rejected"},
		//{"../outsidectx", "dirx", "relative source pattern outside context directory was not rejected"},
	} {
		ctxDir, rootfs := createFiles(t)
		defer deleteFiles(ctxDir, rootfs)
		opts := NewFSOptions(true)
		testee := NewFsBuilder(opts)
		testee.AddFiles(filepath.Join(ctxDir, c.src), c.dest, nil)
		if err := testee.Write(newWriterMock(t)); err == nil {
			t.Errorf(c.msg + ": " + c.src + " -> " + c.dest)
		}
	}
}*/

func mtreeToExpectedPathSet(t *testing.T, rootPath, dhStr string) (r map[string]bool) {
	r = map[string]bool{}
	r["/"] = true
	if rootPath != "" {
		// Add root dirs
		names := strings.Split(filepath.Clean(rootPath), string(filepath.Separator))[1:]
		for i, _ := range names {
			r[string(filepath.Separator)+filepath.Join(names[:i]...)] = true
		}
	}
	dhStr = strings.Replace(dhStr, "$ROOT", rootPath, -1)
	dh, err := mtree.ParseSpec(strings.NewReader(dhStr))
	require.NoError(t, err)
	diff, err := mtree.Compare(&mtree.DirectoryHierarchy{}, dh, testutils.MtreeTestkeywords)
	require.NoError(t, err)
	for _, e := range diff {
		r[filepath.Join(rootPath, string(filepath.Separator)+e.Path())] = true
	}
	return r
}
