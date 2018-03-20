package builder

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
	//"github.com/mgoltzsche/cntnr/pkg/sliceutils"
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
	"xattr",
}

func TestFileSystemBuilder(t *testing.T) {
	ctxDir, rootfs := createFiles(t)
	defer deleteFiles(ctxDir, rootfs)
	testee := NewFileSystemBuilder(rootfs, log.New(os.Stdout, "", 0))
	err := testee.Add(ctxDir, []string{"/dir2"}, "dirx")
	require.NoError(t, err)
	err = testee.Add(ctxDir, []string{"/dir1/file1"}, "/bin/fn")
	require.NoError(t, err)
	err = testee.Add(ctxDir, []string{"/dir1/file2"}, "/")
	require.NoError(t, err)
	err = testee.Add(ctxDir, []string{"/dir1/file*"}, "/bin/")
	require.NoError(t, err)
	err = testee.Add(ctxDir, []string{"/dir1", "/link*"}, "dirp/")
	require.NoError(t, err)
	err = testee.Add(ctxDir, []string{"/dir1", "/link*"}, "dirp/")
	require.NoError(t, err)
	expectedStr := `
		# .
		. size=4096 type=dir mode=0700
		    file2 size=52 mode=0644
		# bin
		bin type=dir mode=0755
		    file1 mode=0444
		    file2 mode=0644
		    fn mode=0444
		..
		# dirp
		dirp size=4096 type=dir mode=0755
		    link1 size=10 type=link mode=0777 link=/dir2
		    link2 size=42 type=link mode=0777 link=dir2
			link3 type=link mode=0777 link=non-existing
		# dirp/dir1
		dir1 size=4096 type=dir mode=0775
		    file1 size=52 mode=0444
		    file2 size=52 mode=0644
		# dirp/dir1/sdir1
		sdir1 size=4096 type=dir mode=0755
		    linkInvalid size=41 type=link mode=0777 link=../../../dir2
		..
		..
		..
		# dirx
		dirx size=4096 type=dir mode=0775
		# dirx/sdir2
		sdir2 size=4096 type=dir mode=0775
		    sfile1 size=59 mode=0444
		    sfile2 size=59 mode=0664
			link4 size=41 type=link mode=0777 link=../../dir2
		..
		..
		..
	`
	expected, err := mtree.ParseSpec(strings.NewReader(expectedStr))
	require.NoError(t, err)
	assertFsState(rootfs, expected, t)
}

func TestFileSystemBuilderRootfsBoundsViolatingLink(t *testing.T) {
	ctxDir, rootfs := createFiles(t)
	defer deleteFiles(ctxDir, rootfs)
	testee := NewFileSystemBuilder(rootfs, log.New(os.Stdout, "", 0))
	err := testee.Add(ctxDir, []string{"/dir1/sdir1/linkInvalid"}, "dirx")
	if err == nil {
		t.Errorf("linking outside rootfs directory was not rejected")
	}
}

func TestFileSystemBuilderContextPathBounds(t *testing.T) {
	ctxDir, rootfs := createFiles(t)
	defer deleteFiles(ctxDir, rootfs)
	testee := NewFileSystemBuilder(rootfs, log.New(os.Stdout, "", 0))
	err := testee.Add(ctxDir, []string{"/dir1/../.."}, "dirx")
	if err == nil {
		t.Errorf("source pattern /dir1/../.. was not rejected")
	}
}

// TODO: security test: rejection of paths outside rootfs (link destination or copy destination)

func createFiles(t *testing.T) (ctxDir, root string) {
	ctxDir, err := ioutil.TempDir("", ".cntnr-test-fs-ctx-")
	require.NoError(t, err)
	root, err = ioutil.TempDir("", ".cntnr-test-fs-root-")
	require.NoError(t, err)
	err = os.MkdirAll(filepath.Join(ctxDir, "dir1"), 0775)
	require.NoError(t, err)
	err = os.MkdirAll(filepath.Join(ctxDir, "dir1", "sdir1"), 0755)
	require.NoError(t, err)
	err = os.MkdirAll(filepath.Join(ctxDir, "dir2", "sdir2"), 0775)
	require.NoError(t, err)
	createFile(filepath.Join(ctxDir, "script.sh"), 0544)
	createFile(filepath.Join(ctxDir, "dir1", "file1"), 0444)
	createFile(filepath.Join(ctxDir, "dir1", "file2"), 1444)
	createFile(filepath.Join(ctxDir, "dir2", "sdir2", "sfile1"), 0444)
	createFile(filepath.Join(ctxDir, "dir2", "sdir2", "sfile2"), 0664)
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

func assertFsState(rootfs string, expected *mtree.DirectoryHierarchy, t *testing.T) {
	dh, err := mtree.Walk(rootfs, nil, mtreeTestkeywords, fseval.DefaultFsEval)
	require.NoError(t, err)
	diff, err := mtree.Compare(expected, dh, mtreeTestkeywords)
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
	}
}
