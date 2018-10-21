package writer

import (
	"bytes"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/mgoltzsche/ctnr/pkg/fs"
	"github.com/mgoltzsche/ctnr/pkg/fs/source"
	"github.com/mgoltzsche/ctnr/pkg/fs/testutils"
	"github.com/mgoltzsche/ctnr/pkg/idutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTarWriter(t *testing.T) {
	tmpDir, err := ioutil.TempDir("", "test-tar-writer-")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)
	extDir := filepath.Join(tmpDir, "extracted")
	err = os.Mkdir(extDir, 0755)
	require.NoError(t, err)
	tarFile := filepath.Join(tmpDir, "archive.tar")
	f, err := os.OpenFile(tarFile, os.O_CREATE|os.O_RDWR, 0644)
	require.NoError(t, err)
	defer f.Close()

	testee := NewTarWriter(f)
	defer testee.Close()

	// Test write operations
	times := fs.FileTimes{}
	times.Mtime, err = time.Parse(time.RFC3339, "2018-02-23T01:01:42Z")
	require.NoError(t, err)
	times.Mtime = time.Unix(times.Mtime.Unix(), 123)
	times.Atime, err = time.Parse(time.RFC3339, "2018-01-23T01:01:43Z")
	require.NoError(t, err)
	usr := idutils.UserIds{uint(os.Getuid()), uint(os.Getgid())}
	dirAttrs := fs.FileAttrs{Mode: os.ModeDir | 0755, UserIds: usr, FileTimes: times}
	dirAttrs2 := dirAttrs
	dirAttrs2.Mode = os.ModeDir | 0775
	fileAttrsA := fs.NodeAttrs{NodeInfo: fs.NodeInfo{fs.TypeFile, fs.FileAttrs{Mode: 0640, UserIds: usr, Size: 1, FileTimes: times}}}
	fileAttrsB := fs.NodeAttrs{NodeInfo: fs.NodeInfo{fs.TypeFile, fs.FileAttrs{Mode: 0755, UserIds: usr, Size: 3, FileTimes: times}}}
	linkAttrs := fs.FileAttrs{Mode: os.ModeSymlink, UserIds: usr, FileTimes: times}
	fifoAttrs := fs.DeviceAttrs{fs.FileAttrs{Mode: syscall.S_IFIFO | 0640, UserIds: usr, FileTimes: times}, 1, 1}
	fileA := source.NewSourceFile(fs.NewReadableBytes([]byte("a")), fileAttrsA.FileAttrs)
	fileB := source.NewSourceFile(fs.NewReadableBytes([]byte("bbb")), fileAttrsB.FileAttrs)
	err = testee.Dir("/", "", dirAttrs2)
	require.NoError(t, err)
	err = testee.Dir("etc", "", dirAttrs2)
	require.NoError(t, err)
	reader, err := testee.File("etc/fileA", fileA)
	require.NoError(t, err)
	assert.Equal(t, fileA, reader, "returned FileSource of File()")
	_, err = testee.File("/etc/fileB", fileB)
	require.NoError(t, err)
	err = testee.Fifo("/etc/fifo", fifoAttrs)
	require.NoError(t, err)
	err = testee.Device("/etc/devA", fifoAttrs)
	require.NoError(t, err)
	err = testee.Link("/etc/link-abs", "/etc/fileA")
	require.NoError(t, err)
	err = testee.Link("/etc/link-rel", "../etc/fileA")
	require.NoError(t, err)
	err = testee.Dir("/dir", "", dirAttrs)
	require.NoError(t, err)
	_, err = testee.File("/dir/file1", fileA)
	require.NoError(t, err)
	_, err = testee.File("/dir/file2", fileB)
	require.NoError(t, err)
	err = testee.Dir("/dir/ndir", "", fs.FileAttrs{Mode: os.ModeDir | 0754, UserIds: usr})
	require.NoError(t, err)
	_, err = testee.File("/dir/ndir/nestedfile", fileA)
	require.NoError(t, err)
	linkAttrs.Symlink = "../etc/fileA"
	err = testee.Symlink("etc/symlink-rel", linkAttrs)
	require.NoError(t, err)
	linkAttrs.Symlink = "/etc/fileB"
	err = testee.Symlink("etc/symlink-abs", linkAttrs)
	require.NoError(t, err)

	// Test paths sanitized
	linkAttrs.Symlink = filepath.Join("..", "..")
	err = testee.Symlink("etc/symlink-sanitized", linkAttrs)
	require.NoError(t, err)
	_, err = testee.File("/../filepathsanitized", fileA)
	require.NoError(t, err)

	err = testee.Close()
	require.NoError(t, err)

	// Untar archive and compare file system without timestamps using tar
	cmd := exec.Command("tar", "-xf", tarFile)
	cmd.Dir = extDir
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err = cmd.Run()
	require.NoError(t, err, buf.String())
	// Cannot test time metadata here since tar -xf does not preserve it
	// for every file type
	testutils.AssertFsState(t, extDir, "", testutils.MtreeTestkeywords, `
		# .
		. size=4096 type=dir mode=0775
			filepathsanitized mode=0640
		# dir
		dir type=dir mode=0755
		    file1 mode=0640
		    file2 mode=0755
		
		# dir/ndir
		ndir type=dir mode=0754
			nestedfile type=file mode=0640
		..
		..
		# bin
		etc type=dir mode=0775
			devA mode=0640
			fifo mode=0640
			fileA mode=0640
			fileB mode=0755
			link-abs mode=0640 nlink=3
			link-rel mode=0640 nlink=3
			symlink-abs type=link mode=0777 link=$ROOT/etc/fileB
			symlink-rel type=link mode=0777 link=../etc/fileA
			symlink-sanitized type=link mode=0777 link=..
		..
	`)
	// Test mtime applied to file
	// (Compare only unix seconds since tar does not provide nanoseconds)
	file1 := filepath.Join(extDir, "dir/file1")
	st, err := os.Stat(file1)
	require.NoError(t, err)
	assert.Equal(t, times.Mtime.Unix(), st.ModTime().Unix(), "mtime should be applied")
}
