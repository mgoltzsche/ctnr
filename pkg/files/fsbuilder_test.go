package files

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/openSUSE/umoci/pkg/fseval"
	"github.com/stretchr/testify/require"
	"github.com/vbatts/go-mtree"
)

var mtreeTestkeywords = []mtree.Keyword{
	//"size",
	"type",
	"uid",
	"gid",
	"mode",
	"link",
	"nlink",
	"xattr",
}

func absDirs(baseDir string, file []string) []string {
	files := make([]string, len(file))
	for i, f := range file {
		files[i] = filepath.Join(baseDir, f)
	}
	return files
}

func TestFsBuilder(t *testing.T) {
	tmpDir, rootfs := createTestFileSystem(t)
	defer os.RemoveAll(tmpDir)

	tarFile := filepath.Join(tmpDir, "archive.tar")
	tarDir(t, rootfs, "-cf", tarFile)

	// Test add all
	destfs := filepath.Join(tmpDir, "destfs")
	opts := NewFSOptions(true)
	warn := log.New(os.Stdout, "warn: ", 0)
	testee := NewFsBuilder(opts)
	testee.AddAll(rootfs, []string{"."}, "/", nil)

	writer := NewDirWriter(destfs, opts, warn)
	err := testee.Write(writer)
	require.NoError(t, err)
	assertFsState(t, destfs, "", expectedTestfsState)

	// Test hash
	hash1, err := testee.Hash()
	require.NoError(t, err)
	if string(hash1) == "" {
		t.Error("returned empty hash")
		t.FailNow()
	}

	// Test add files
	srcFile := filepath.Join(tmpDir, "sourcefile")
	err = ioutil.WriteFile(srcFile, []byte("content"), 0644)
	require.NoError(t, err)
	testee.AddFiles(srcFile, "addedfile", nil)
	err = testee.Write(writer)
	require.NoError(t, err)
	if fi, err := os.Stat(filepath.Join(destfs, "addedfile")); err != nil || !fi.Mode().IsRegular() {
		t.Errorf("should write 'addedfile' but returned error or did not write regular file. err: %s", err)
		t.FailNow()
	}
	testee.AddFiles(srcFile, "addDir/", nil)
	err = testee.Write(writer)
	require.NoError(t, err)
	if fi, err := os.Stat(filepath.Join(destfs, "addDir/sourcefile")); err != nil || !fi.Mode().IsRegular() {
		t.Errorf("should write 'sourcefile' into 'addDir/' but returned error or did not write regular file. err: %s", err)
		t.FailNow()
	}

	// Test hash changed
	hash2, err := testee.Hash()
	require.NoError(t, err)
	if hash1 == hash2 {
		t.Errorf("hash did not change after files changed")
	}

	// TODO: test and support URL source and archive

	/*ctxDir, rootfs := createFiles(t)
	defer deleteFiles(ctxDir, rootfs)
	warn := log.New(os.Stdout, "", 0)
	opts := NewFSOptions(true)
	testee := NewFsBuilder(opts)
	err := os.Mkdir(filepath.Join(rootfs, "dirp"), 0750)
	require.NoError(t, err)
	apply := func() {
		for _, p := range []struct {
			src  string
			dest string
			usr  *idutils.UserIds
		}{
			{"dir2", "dirx", nil},
			{"dir1", "dirp/dir1", nil},
			{"dir1/", "dirp/dir1", nil},
			{"dir1/file1", "/bin/fn", nil},
			{"dir1/file2/", "/file2", nil},
			{"dir1/file1", "dirp/file1", &idutils.UserIds{0, 0}},
			{"link1", "dirp/link1", nil},
			{"link2", "dirp/link2", nil},
			{"link3/", "dirp/link3", nil},
		} {
			testee.AddFiles(filepath.Join(ctxDir, p.src), p.dest, p.usr)
		}
	}
	apply()

	// Test Hash()
	hash1, err := testee.Hash()
	require.NoError(t, err)
	testee = NewFsBuilder(opts)
	apply()
	hash2, err := testee.Hash()
	require.NoError(t, err)
	if hash1 != hash2 {
		t.Errorf("Hash(): must be idempotent")
		t.FailNow()
	}
	testee.AddFiles(filepath.Join(ctxDir, "/dir1/file2"), "/bin/fn", nil)
	hash2, err = testee.Hash()
	require.NoError(t, err)
	if hash1 == hash2 {
		t.Errorf("Hash(): must return changed value when content changed")
		t.FailNow()
	}
	testee = NewFsBuilder(opts)
	apply()

	// Test Write()
	w := NewDirWriter(rootfs, opts, warn)
	err = testee.Write(w)
	require.NoError(t, err)
	assertFsState(t, rootfs, "", `
		# .
		. size=4096 type=dir mode=0700
		    file2 size=52 mode=0754
		# bin
		bin type=dir mode=0755
		    fn mode=0444
		..
		# dirp
		dirp size=4096 type=dir mode=0750
			file1 mode=0444
		    link1 size=10 type=link mode=0777 link=/dir2
		    link2 size=42 type=link mode=0777 link=dir2
			link3 type=link mode=0777 link=non-existing
		# dirp/dir1
		dir1 size=4096 type=dir mode=0754
		    file1 size=52 mode=0444
		    file2 size=52 mode=0754
		# dirp/dir1/sdir1
		sdir1 size=4096 type=dir mode=0700
		    linkInvalid size=41 type=link mode=0777 link=../../../dir2
		..
		..
		..
		# dirx
		dirx size=4096 type=dir mode=0755
		# dirx/sdir2
		sdir2 size=4096 type=dir mode=0754
		    sfile1 size=59 mode=0444
		    sfile2 size=59 mode=0640
			link4 size=41 type=link mode=0777 link=../../dir2
		..
		..
	`)*/
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

func createFiles(t *testing.T) (ctxDir, root string) {
	ctxDir, err := ioutil.TempDir("", ".cntnr-test-fs-ctx-")
	require.NoError(t, err)
	root, err = ioutil.TempDir("", ".cntnr-test-fs-root-")
	require.NoError(t, err)
	err = os.MkdirAll(filepath.Join(ctxDir, "dir1"), 0754)
	require.NoError(t, err)
	err = os.Mkdir(filepath.Join(ctxDir, "dir1", "sdir1"), 0700)
	require.NoError(t, err)
	err = os.Mkdir(filepath.Join(ctxDir, "dir2"), 0755)
	require.NoError(t, err)
	err = os.Mkdir(filepath.Join(ctxDir, "dir2", "sdir2"), 0754)
	require.NoError(t, err)
	createFile(filepath.Join(ctxDir, "script.sh"), 0544)
	createFile(filepath.Join(ctxDir, "dir1", "file1"), 0444)
	createFile(filepath.Join(ctxDir, "dir1", "file2"), 0754)
	createFile(filepath.Join(ctxDir, "dir2", "sdir2", "sfile1"), 0444)
	createFile(filepath.Join(ctxDir, "dir2", "sdir2", "sfile2"), 0640)
	// TODO: make mode 770 work (currently write permissions are not set on group/others when writing dir/file)
	//st, _ := os.Stat(filepath.Join(ctxDir, "dir2"))
	//panic(st.Mode().String())
	createSymlink(filepath.Join(ctxDir, "link1"), "/dir2")
	createSymlink(filepath.Join(ctxDir, "link2"), "dir2")
	createSymlink(filepath.Join(ctxDir, "link3"), "non-existing")
	createSymlink(filepath.Join(ctxDir, "dir2", "sdir2", "link4"), "../../dir2")
	createSymlink(filepath.Join(ctxDir, "dir1", "sdir1", "linkInvalid"), "../../../dir2")
	return
}

func deleteFiles(ctxDir, rootfs string) {
	os.RemoveAll(ctxDir)
	os.RemoveAll(rootfs)
}

func createFile(name string, mode os.FileMode) {
	if err := ioutil.WriteFile(name, []byte(name+" content"), mode); err != nil {
		panic(err)
	}
}

func createSymlink(name, dest string) {
	if err := os.Symlink(dest, name); err != nil {
		panic(err)
	}
}

func assertFsState(t *testing.T, rootfs, rootPath, expectedDirectoryHierarchy string) {
	expectedDirectoryHierarchy = strings.Replace(expectedDirectoryHierarchy, "$ROOT", rootPath, -1)
	expectedDh, err := mtree.ParseSpec(strings.NewReader(expectedDirectoryHierarchy))
	require.NoError(t, err)
	dh, err := mtree.Walk(rootfs, nil, mtreeTestkeywords, fseval.DefaultFsEval)
	require.NoError(t, err)
	diff, err := mtree.Compare(expectedDh, dh, mtreeTestkeywords)
	require.NoError(t, err)
	if len(diff) > 0 {
		var buf bytes.Buffer
		_, err = dh.WriteTo(&buf)
		require.NoError(t, err)
		fmt.Println(string(buf.Bytes()))
		s := make([]string, len(diff))
		for i, c := range diff {
			s[i] = c.String()
		}
		sort.Strings(s)
		t.Error("Unexpected rootfs differences:\n  " + strings.Join(s, "\n  "))
		t.Fail()
	}
}
