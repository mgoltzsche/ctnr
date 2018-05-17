package source

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/mgoltzsche/cntnr/pkg/fs"
	"github.com/mgoltzsche/cntnr/pkg/fs/testutils"
	"github.com/openSUSE/umoci/pkg/fseval"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func expectedWrittenPaths(prefix string) []string {
	usr := ""
	uid := os.Geteuid()
	gid := os.Getegid()
	if uid != 0 || gid != 0 {
		usr = fmt.Sprintf(" usr=%d:%d", uid, gid)
	}
	return []string{
		filepath.Clean(prefix+string(os.PathSeparator)) + " type=dir" + usr + " mode=755",
		prefix + "/etc type=dir" + usr + " mode=755",
		prefix + "/etc/_symlink type=symlink" + usr + " link=fileA",
		prefix + "/etc/dirA type=dir" + usr + " mode=755",
		prefix + "/etc/dirB type=dir" + usr + " mode=750",
		prefix + "/etc/dirB/fileC type=file" + usr + " mode=440 size=7 hash=sha256:ed7002b439e9ac845f22357d822bac1444730fbdb6016d3ec9432297b9ec9f73",
		prefix + "/etc/fifo type=fifo" + usr + " mode=640",
		prefix + "/etc/fileA type=file" + usr + " mode=644 size=7 hash=sha256:ed7002b439e9ac845f22357d822bac1444730fbdb6016d3ec9432297b9ec9f73",
		// ATTENTION: link order not guaranteed - depends on external tool
		prefix + "/etc/fileB hlink=" + prefix + "/etc/link",
		prefix + "/etc/link type=file" + usr + " mode=750 size=7 hash=sha256:ed7002b439e9ac845f22357d822bac1444730fbdb6016d3ec9432297b9ec9f73",
	}
}

func TestSourceTar(t *testing.T) {
	tmpDir, rootfs := createTestFileSystem(t)
	defer os.RemoveAll(tmpDir)
	tarFile1 := filepath.Join(tmpDir, "a1.tar")
	tarFile2 := filepath.Join(tmpDir, "a2.tar")
	tarDir(t, rootfs, "-cf", tarFile1)
	changedFile := filepath.Join(rootfs, "changedfile")
	err := ioutil.WriteFile(changedFile, []byte("data"), 0644)
	require.NoError(t, err)
	tarDir(t, rootfs, "-cf", tarFile2)

	// Test attributes
	testee := NewSourceTar(tarFile1)
	a := testee.Attrs()
	if a.NodeType != fs.TypeOverlay {
		t.Error("type != TypeOverlay")
		t.FailNow()
	}
	wa, err := testee.DerivedAttrs()
	require.NoError(t, err)
	hash1 := wa.Hash
	testee = NewSourceTar(tarFile2)
	wa, err = testee.DerivedAttrs()
	require.NoError(t, err)
	hash2 := wa.Hash
	if hash1 == hash2 {
		t.Error("hash1 == hash2")
	}
	testee = NewSourceTar(tarFile1)
	wa, err = testee.DerivedAttrs()
	require.NoError(t, err)
	hash2 = wa.Hash
	if hash1 != hash2 {
		t.Error("hash1 != hash1")
	}

	// Test extraction
	// ATTENTION: Time metadata cannot be tested here due to comparison with external tar tool. Must be tested in fsbuilder test
	destPath := filepath.Join("/a", "b")
	w := testutils.NewWriterMock(t, fs.AttrsHash)
	err = testee.Write(destPath, "", &testutils.ExpandingWriterMock{w}, nil)
	require.NoError(t, err)
	sort.Strings(w.Written)
	expected := expectedWrittenPaths("/a/b")
	written := "\n  " + strings.Join(w.Written, "\n  ")
	assert.True(t, len(expected) == len(w.Written), "extracted tar source: "+written)
	assert.Equal(t, expected[:len(expected)-2], w.Written[:len(w.Written)-2], "extracted tar source")
	if strings.Index(w.Written[len(w.Written)-1], "/a/b/etc/link hlink=/a/b/etc/fileB") != 0 &&
		strings.Index(w.Written[len(w.Written)-2], "/a/b/etc/fileB hlink=/a/b/etc/link") != 0 {
		t.Errorf("did not write hardlinks properly: " + written)
	}
}

func prefixedPaths(paths []string, prefix string) []string {
	r := []string{}
	for _, line := range paths {
		r = append(r, prefix+line)
	}
	return r
}

func createTestFileSystem(t *testing.T) (tmpDir string, rootfs string) {
	mtime, err := time.Parse(time.RFC3339, "2018-01-23T01:01:42Z")
	require.NoError(t, err)
	mtime = time.Unix(mtime.Unix(), 900000000)
	times := fs.FileTimes{Mtime: mtime}
	tmpDir, err = ioutil.TempDir("", "testfs-")
	require.NoError(t, err)
	rootfs = filepath.Join(tmpDir, "rootfs")
	createTestFile(t, rootfs, "/etc/fileA", fs.FileAttrs{Mode: 0644, FileTimes: times})
	createTestFile(t, rootfs, "/etc/fileB", fs.FileAttrs{Mode: 0750, FileTimes: times})
	dirA := filepath.Join(rootfs, "/etc/dirA")
	err = os.Mkdir(dirA, 0755)
	require.NoError(t, err)
	err = fseval.RootlessFsEval.Lutimes(dirA, time.Now(), mtime)
	require.NoError(t, err)
	dirB := filepath.Join(rootfs, "/etc/dirB")
	err = os.Mkdir(dirB, 0750)
	require.NoError(t, err)
	err = fseval.RootlessFsEval.Lutimes(dirB, time.Now(), mtime)
	require.NoError(t, err)
	createTestFile(t, rootfs, "/etc/dirB/fileC", fs.FileAttrs{Mode: 0440, FileTimes: times})
	symlink := filepath.Join(rootfs, "/etc/_symlink")
	err = os.Symlink("fileA", symlink)
	require.NoError(t, err)
	err = fseval.RootlessFsEval.Lutimes(symlink, time.Now(), mtime)
	require.NoError(t, err)
	err = os.Link(filepath.Join(rootfs, "/etc/fileB"), filepath.Join(rootfs, "/etc/link"))
	require.NoError(t, err)
	err = unix.Mknod(filepath.Join(rootfs, "/etc/fifo"), syscall.S_IFIFO|0640, 0)
	require.NoError(t, err)
	return
}

func createTestFile(t *testing.T, fs, file string, a fs.FileAttrs) {
	file = filepath.Join(fs, file)
	err := os.MkdirAll(filepath.Dir(file), 0755)
	require.NoError(t, err)
	err = ioutil.WriteFile(file, []byte("content"), a.Mode.Perm())
	require.NoError(t, err)
	err = fseval.RootlessFsEval.Lutimes(file, time.Now(), a.Mtime)
	require.NoError(t, err)
}

func tarDir(t *testing.T, dir, opts, tarFile string) {
	cmd := exec.Command("tar", opts, tarFile, ".")
	cmd.Dir = dir
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	require.NoError(t, err, buf.String())
}
