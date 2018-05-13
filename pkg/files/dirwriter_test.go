package files

import (
	"bytes"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/mgoltzsche/cntnr/pkg/idutils"
	"github.com/stretchr/testify/require"
)

// TODO: check fifo type properly
const expectedTestfsState = `
	# .
	. size=4096 type=dir mode=0755
		filefrominvalidpath mode=0640
		filefrominvalidsymlink mode=0640
	# bin
	etc type=dir mode=0775
		blockA mode=0640
		fifo mode=0640
		_dirReplacedWithFile mode=0640
		_dirReplacedWithLink type=link mode=0777 link=$ROOT/etc/dirB
		_linkReplacedWithFile mode=0640
		_linkLazyReplacedWithFile mode=0640
		_symlinkReplacedWithFile mode=0640
		fileA mode=0640 nlink=3
		fileB mode=0640
		link-abs mode=0640 nlink=3
		link-rel mode=0640 nlink=3
		symlink-abs type=link mode=0777 link=$ROOT/etc/dirB
		symlink-rel type=link mode=0777 link=../etc/dirA
		symlink-nonexist type=link mode=0777 link=$ROOT/etc/pdir
	# etc/_fileReplacedWithDir
	_fileReplacedWithDir type=dir mode=0750
	..
	# etc/_linkReplacedWithDir
	_symlinkReplacedWithDir type=dir mode=0750
	..
	# etc/dirA
	dirA type=dir mode=0750
		symlinkResolved1 mode=0640
	# etc/dirA/dirA1
	dirA1 type=dir mode=0750
		symlinkResolved2 mode=0640
	..
	..
	# etc/dirB
	dirB type=dir mode=0750
	# etc/dirB/dirB1
	dirB1 type=dir mode=0750
	..
	..
	# etc/dirtoberemovedandrewritten
	dirtoberemovedandrewritten type=dir mode=0750
	..
	# etc/dirx
	dirX type=dir mode=0755
	..
	# etc/dirp
	pdir type=dir mode=0755
	..
	..
`

func TestDirWriter(t *testing.T) {
	tmpDir, rootfs := createTestFileSystem(t)
	defer os.RemoveAll(tmpDir)

	// TODO: check if node insertion must happen orderly to guarantee hard link order
	assertFsState(t, rootfs, "", expectedTestfsState)
}

func createTestFileSystem(t *testing.T) (tmpDir string, rootfs string) {
	success := false
	tmpDir, err := ioutil.TempDir("", "cntnr-test-dirfswriter-")
	require.NoError(t, err)
	defer func() {
		if !success {
			os.RemoveAll(tmpDir)
		}
	}()
	opts := NewFSOptions(true)
	warn := log.New(os.Stdout, "warn: ", 0)
	rootfs = filepath.Join(tmpDir, "rootfs")
	fileOutsideRootfs := filepath.Join(tmpDir, "outsiderootfs")

	testee := NewDirWriter(rootfs, opts, warn)

	err = ioutil.WriteFile(fileOutsideRootfs, []byte{}, 0644)
	require.NoError(t, err)

	// Test basic file, directory, link, fifo, block writes
	defDirAttrs := FileAttrs{Mode: os.ModeDir | 0755, UserIds: idutils.UserIds{0, 0}}
	dirAttrs := FileAttrs{Mode: os.ModeDir | 0750, UserIds: idutils.UserIds{0, 0}}
	fileAttrs := FileAttrs{Mode: 0640, UserIds: idutils.UserIds{0, 0}}
	linkAttrs := FileAttrs{Mode: os.ModeSymlink | 0777, UserIds: idutils.UserIds{0, 0}}
	fifoAttrs := FileAttrs{Mode: syscall.S_IFIFO | 0640, UserIds: idutils.UserIds{0, 0}}
	srcA := bytes.NewReader([]byte("a"))
	srcB := bytes.NewReader([]byte("b"))
	err = testee.Dir("etc", FileAttrs{Mode: os.ModeDir | 0775, UserIds: idutils.UserIds{0, 0}})
	require.NoError(t, err)
	err = testee.File("etc/fileA", srcA, fileAttrs)
	require.NoError(t, err)
	err = testee.File("/etc/fileB", srcB, fileAttrs)
	require.NoError(t, err)
	err = testee.Dir("etc/dirA", dirAttrs)
	require.NoError(t, err)
	err = testee.Dir("/etc/dirB", dirAttrs)
	require.NoError(t, err)
	err = testee.Fifo("/etc/fifo", fifoAttrs)
	require.NoError(t, err)
	err = testee.Block("/etc/blockA", fifoAttrs)
	require.NoError(t, err)
	linkAttrs.Link = "/etc/fileA"
	err = testee.Link("/etc/link-abs", linkAttrs)
	require.NoError(t, err)
	linkAttrs.Link = "../etc/fileA"
	err = testee.Link("/etc/link-rel", linkAttrs)
	require.NoError(t, err)

	// Test symlink resolution
	linkAttrs.Link = "../etc/dirA"
	err = testee.Symlink("etc/symlink-rel", linkAttrs)
	require.NoError(t, err)
	linkAttrs.Link = "/etc/dirB"
	err = testee.Symlink("etc/symlink-abs", linkAttrs)
	require.NoError(t, err)
	err = testee.Dir("etc/symlink-rel/dirA1", dirAttrs)
	require.NoError(t, err)
	err = testee.Dir("etc/symlink-abs/dirB1", dirAttrs)
	require.NoError(t, err)
	err = testee.File("etc/symlink-rel/symlinkResolved1", srcA, fileAttrs)
	require.NoError(t, err)
	err = testee.File("/etc/symlink-rel/dirA1/symlinkResolved2", srcA, fileAttrs)
	require.NoError(t, err)

	// Test replacements
	linkAttrs.Link = "/etc/dirB"
	err = testee.Dir("etc/_dirReplacedWithFile", dirAttrs)
	require.NoError(t, err)
	err = testee.File("etc/_dirReplacedWithFile", srcA, fileAttrs)
	require.NoError(t, err)
	err = testee.File("etc/_fileReplacedWithDir", srcA, fileAttrs)
	require.NoError(t, err)
	err = testee.Dir("etc/_fileReplacedWithDir", dirAttrs)
	require.NoError(t, err)
	err = testee.Dir("etc/_dirReplacedWithLink", linkAttrs)
	require.NoError(t, err)
	err = testee.Symlink("etc/_dirReplacedWithLink", linkAttrs)
	require.NoError(t, err)
	err = testee.Symlink("etc/_symlinkReplacedWithFile", linkAttrs)
	require.NoError(t, err)
	err = testee.File("etc/_symlinkReplacedWithFile", srcA, fileAttrs)
	require.NoError(t, err)
	err = testee.Symlink("etc/_symlinkReplacedWithDir", linkAttrs)
	require.NoError(t, err)
	err = testee.Dir("etc/_symlinkReplacedWithDir", dirAttrs)
	require.NoError(t, err)
	linkAttrs.Link = "/etc/symlink-rel/symlinkResolved1"
	err = testee.Link("etc/_linkReplacedWithFile", linkAttrs)
	require.NoError(t, err)
	err = testee.File("etc/_linkReplacedWithFile", srcA, fileAttrs)
	require.NoError(t, err)
	err = testee.Link("/etc/_linkLazyReplacedWithFile", linkAttrs)
	require.NoError(t, err)
	err = testee.File("/etc/_linkLazyReplacedWithFile", srcB, fileAttrs)
	require.NoError(t, err)

	// Test implicit dir
	err = testee.DirImplicit("etc/dirA", defDirAttrs) // should not override existing dir's attrs
	require.NoError(t, err)
	err = testee.DirImplicit("etc/dirX", defDirAttrs) // should create non-existing dir
	require.NoError(t, err)
	linkAttrs.Link = "/etc/pdir"
	err = testee.Symlink("etc/symlink-nonexist", linkAttrs)
	require.NoError(t, err)
	err = testee.DirImplicit("etc/symlink-nonexist", defDirAttrs) // should create not existing dir also when link destination
	require.NoError(t, err)

	// Test remove
	err = testee.Dir("etc/dirtoberemoved", dirAttrs)
	require.NoError(t, err)
	err = testee.Remove("etc/dirtoberemoved")
	require.NoError(t, err)
	err = testee.File("etc/filetoberemoved", srcA, fileAttrs)
	require.NoError(t, err)
	err = testee.Dir("etc/dirtoberemovedandrewritten", dirAttrs)
	require.NoError(t, err)
	err = testee.Remove("etc/dirtoberemovedandrewritten")
	require.NoError(t, err)
	err = testee.Dir("etc/dirtoberemovedandrewritten", dirAttrs)
	require.NoError(t, err)
	err = testee.Remove("etc/filetoberemoved")
	require.NoError(t, err)
	err = testee.File("etc/symlink-rel/dirA1/filetoberemoved", srcA, fileAttrs)
	require.NoError(t, err)
	err = testee.Remove("etc/symlink-rel/dirA1/filetoberemoved")
	require.NoError(t, err)
	err = testee.Symlink("etc/symlink-toberemoved", linkAttrs)
	require.NoError(t, err)
	err = testee.Remove("etc/symlink-toberemoved")
	require.NoError(t, err)
	err = testee.Symlink("etc/link-toberemoved", linkAttrs)
	require.NoError(t, err)
	err = testee.Remove("etc/link-toberemoved")
	require.NoError(t, err)

	// Test file system boundaries
	err = testee.File("/../filefrominvalidpath", srcA, fileAttrs)
	require.NoError(t, err)
	linkAttrs.Link = filepath.Join("..", "..")
	err = testee.Symlink("etc/symlink-invalid", linkAttrs)
	if err == nil {
		t.Errorf("should return error when symlinking outside rootfs boundaries (/..)")
		t.FailNow()
	}
	invalidSymlinkFile := filepath.Join(rootfs, "etc", "symlink-invalid")
	err = os.Symlink(linkAttrs.Link, invalidSymlinkFile)
	require.NoError(t, err)
	err = testee.File("etc/symlink-invalid/filefrominvalidsymlink", srcA, fileAttrs)
	require.NoError(t, err)
	os.Remove(invalidSymlinkFile)

	linkAttrs.Link = fileOutsideRootfs
	err = testee.Link("etc/link-invalid", linkAttrs)
	if err == nil {
		t.Errorf("should return error when linking outside rootfs boundaries")
		t.FailNow()
	}

	// Test testee reuses existing dir
	testee = NewDirWriter(rootfs, opts, warn)
	err = testee.Dir("etc/dirA", dirAttrs)
	require.NoError(t, err)

	success = true
	return
}
