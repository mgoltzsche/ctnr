package testutils

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/mgoltzsche/cntnr/pkg/fs"
	"github.com/mgoltzsche/cntnr/pkg/idutils"
	"github.com/openSUSE/umoci/pkg/fseval"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vbatts/go-mtree"
)

var (
	MtreeTestkeywords = []mtree.Keyword{
		//"size",
		"type",
		"uid",
		"gid",
		"mode",
		"link",
		"nlink",
		"xattr",
	}
	MtreeTestkeywordsWithTarTime = append(MtreeTestkeywords, "tar_time")
)

const ExpectedTestfsState = `
	# .
	. size=4096 type=dir mode=0775 tar_time=1519347702.000000000
		filepathsanitized mode=0640 tar_time=1519347702.000000000
	# dir1
	dir1 type=dir mode=0775 nlink=3 tar_time=1519347702.000000000
	    file1 mode=0640 tar_time=1519347702.000000000
	    file2 mode=0640 tar_time=1519347702.000000000

	# dir1/sdir
	sdir type=dir mode=0775 nlink=3 tar_time=1519347702.000000000
		nestedsymlink type=link mode=0777 link=nesteddir tar_time=1519347702.000000000

	# dir1/sdir/nesteddir tar_time=1519347702.000000000
	nesteddir type=dir mode=0750 nlink=2 tar_time=1519347702.000000000
	..
	..
	..
	# dir2
	dir2 type=dir mode=0775 nlink=2 tar_time=1519347702.000000000
	    file3 mode=0640 tar_time=1519347702.000000000
	..
	# bin
	etc type=dir mode=0775 tar_time=1519347702.000000000
		blockA mode=0640 tar_time=1519347702.000000000
		chrdevA mode=0640 tar_time=1519347702.000000000
		fifo mode=0640 tar_time=1519347702.000000000
		_dirReplacedWithFile mode=0640 tar_time=1519347702.000000000
		_dirReplacedWithSymlink type=link mode=0777 link=$ROOT/etc/dirB tar_time=1519347702.000000000
		_linkReplacedWithFile mode=0640 tar_time=1519347702.000000000
		_symlinkReplacedWithFile mode=0640 tar_time=1519347702.000000000
		fileA mode=0640 nlink=3 tar_time=1519347702.000000000
		fileB mode=0640 tar_time=1519347702.000000000
		link-abs mode=0640 nlink=3 tar_time=1519347702.000000000
		link-rel mode=0640 nlink=3 tar_time=1519347702.000000000
		symlink-abs type=link mode=0777 link=$ROOT/etc/dirB tar_time=1519347702.000000000
		symlink-rel type=link mode=0777 link=../etc/dirA tar_time=1519347702.000000000
		symlink-sanitized type=link mode=0777 link=.. tar_time=1519347702.000000000
	# etc/_fileReplacedWithDir
	_fileReplacedWithDir type=dir mode=0750 tar_time=1519347702.000000000
	..
	# etc/_linkReplacedWithDir
	_symlinkReplacedWithDir type=dir mode=0750 tar_time=1519347702.000000000
	..
	# etc/dirA
	dirA type=dir mode=0750 tar_time=1519347702.000000000
		symlinkResolved1 mode=0640 tar_time=1519347702.000000000
	# etc/dirA/dirA1 tar_time=1519347702.000000000
	dirA1 type=dir mode=0750 tar_time=1519347702.000000000
		symlinkResolved2 mode=0640 tar_time=1519347702.000000000
	..
	..
	# etc/dirB
	dirB type=dir mode=0750 tar_time=1519347702.000000000
	# etc/dirB/dirB1
	dirB1 type=dir mode=0750 tar_time=1519347702.000000000
	..
	..
	# etc/dirtoberemovedandrewritten
	dirtoberemovedandrewritten type=dir mode=0750 tar_time=1519347702.000000000
	..
	..
`

func WriteTestFileSystem(t *testing.T, testee fs.Writer) (tmpDir string, rootfs string) {
	// Test basic file, directory, link, fifo, block writes
	times := fs.FileTimes{}
	var err error
	times.Mtime, err = time.Parse(time.RFC3339, "2018-02-23T01:01:42Z") // unix: 1519347702
	require.NoError(t, err)
	times.Mtime = time.Unix(times.Mtime.Unix(), 123)
	times.Atime, err = time.Parse(time.RFC3339, "2018-01-23T01:01:43Z")
	require.NoError(t, err)
	dirAttrs := fs.FileAttrs{Mode: os.ModeDir | 0750, UserIds: idutils.UserIds{0, 0}, FileTimes: times}
	dirAttrsDef := dirAttrs
	dirAttrsDef.Mode = os.ModeDir | 0775
	fileAttrsA := fs.NodeAttrs{NodeInfo: fs.NodeInfo{fs.TypeFile, fs.FileAttrs{Mode: 0640, UserIds: idutils.UserIds{0, 0}, Size: 1, FileTimes: times}}}
	fileAttrsB := fs.NodeAttrs{NodeInfo: fs.NodeInfo{fs.TypeFile, fs.FileAttrs{Mode: 0640, UserIds: idutils.UserIds{0, 0}, Size: 3, FileTimes: times}}}
	symlinkAttrs := fs.FileAttrs{Mode: os.ModeSymlink, UserIds: idutils.UserIds{0, 0}, FileTimes: times}
	fifoAttrs := fs.DeviceAttrs{fs.FileAttrs{Mode: os.ModeNamedPipe | 0640, UserIds: idutils.UserIds{0, 0}, FileTimes: times}, 1, 1}
	chardevAttrs := fs.DeviceAttrs{fs.FileAttrs{Mode: os.ModeCharDevice | 0640, UserIds: idutils.UserIds{0, 0}, FileTimes: times}, 1, 1}
	blkAttrs := fs.DeviceAttrs{fs.FileAttrs{Mode: os.ModeDevice | 0640, UserIds: idutils.UserIds{0, 0}, FileTimes: times}, 1, 1}
	fileA := NewSourceMock(fs.TypeFile, fileAttrsA.FileAttrs, "")
	fileA.NodeAttrs = fileAttrsA
	fileA.Readable = fs.NewReadableBytes([]byte("a"))
	fileB := NewSourceMock(fs.TypeFile, fileAttrsB.FileAttrs, "")
	fileB.NodeAttrs = fileAttrsB
	fileB.Readable = fs.NewReadableBytes([]byte("bbb"))
	err = testee.Dir("etc", "", dirAttrsDef)
	require.NoError(t, err)
	err = testee.Dir("/", "", dirAttrsDef)
	require.NoError(t, err)
	reader, err := testee.File("etc/fileA", fileA)
	require.NoError(t, err)
	assert.NotNil(t, reader, "reader returned from File(/etc/fileA)")
	_, err = testee.File("/etc/fileB", fileB)
	require.NoError(t, err)
	err = testee.Dir("etc/dirA", "", dirAttrs)
	require.NoError(t, err)
	err = testee.Dir("/etc/dirB", "", dirAttrs)
	require.NoError(t, err)
	err = testee.Fifo("/etc/fifo", fifoAttrs)
	require.NoError(t, err)
	err = testee.Device("/etc/blockA", blkAttrs)
	require.NoError(t, err)
	err = testee.Device("/etc/chrdevA", chardevAttrs)
	require.NoError(t, err)
	err = testee.Link("/etc/link-abs", "/etc/fileA")
	require.NoError(t, err)
	err = testee.Link("/etc/link-rel", "../etc/fileA")
	require.NoError(t, err)
	err = testee.Dir("dir1", "", dirAttrsDef)
	require.NoError(t, err)
	_, err = testee.File("/dir1/file1", fileA)
	require.NoError(t, err)
	_, err = testee.File("/dir1/file2", fileA)
	require.NoError(t, err)
	err = testee.Dir("/dir2", "", dirAttrsDef)
	require.NoError(t, err)
	_, err = testee.File("/dir2/file3", fileB)
	require.NoError(t, err)
	symlinkAttrs.Symlink = "nesteddir"
	err = testee.Symlink("/dir1/sdir/nestedsymlink", symlinkAttrs)
	require.NoError(t, err)
	err = testee.Dir("/dir1/sdir/nesteddir", "", dirAttrs)
	require.NoError(t, err)
	err = testee.Dir("/dir1/sdir", "", dirAttrsDef)
	require.NoError(t, err)

	// Test symlink resolution
	symlinkAttrs.Symlink = "../etc/dirA"
	err = testee.Symlink("etc/symlink-rel", symlinkAttrs)
	require.NoError(t, err)
	symlinkAttrs.Symlink = "/etc/dirB"
	err = testee.Symlink("etc/symlink-abs", symlinkAttrs)
	require.NoError(t, err)
	err = testee.Dir("etc/symlink-rel/dirA1", "", dirAttrs)
	require.NoError(t, err)
	err = testee.Dir("etc/symlink-abs/dirB1", "", dirAttrs)
	require.NoError(t, err)
	_, err = testee.File("etc/symlink-rel/symlinkResolved1", fileA)
	require.NoError(t, err)
	_, err = testee.File("/etc/symlink-rel/dirA1/symlinkResolved2", fileA)
	require.NoError(t, err)

	// Test replacements
	symlinkAttrs.Symlink = "/etc/dirB"
	err = testee.Dir("etc/_dirReplacedWithFile", "", dirAttrs)
	require.NoError(t, err)
	_, err = testee.File("etc/_dirReplacedWithFile", fileA)
	require.NoError(t, err)
	_, err = testee.File("etc/_fileReplacedWithDir", fileA)
	require.NoError(t, err)
	err = testee.Dir("etc/_fileReplacedWithDir", "", dirAttrs)
	require.NoError(t, err)
	err = testee.Symlink("etc/_dirReplacedWithSymlink", symlinkAttrs)
	require.NoError(t, err)
	err = testee.Symlink("etc/_symlinkReplacedWithFile", symlinkAttrs)
	require.NoError(t, err)
	_, err = testee.File("etc/_symlinkReplacedWithFile", fileA)
	require.NoError(t, err)
	err = testee.Symlink("etc/_symlinkReplacedWithDir", symlinkAttrs)
	require.NoError(t, err)
	err = testee.Dir("etc/_symlinkReplacedWithDir", "", dirAttrs)
	require.NoError(t, err)
	err = testee.Link("etc/_linkReplacedWithFile", "/etc/symlink-rel/symlinkResolved1")
	require.NoError(t, err)
	_, err = testee.File("etc/_linkReplacedWithFile", fileA)
	require.NoError(t, err)

	// Test remove
	err = testee.Dir("etc/dirtoberemoved", "", dirAttrs)
	require.NoError(t, err)
	err = testee.Remove("etc/dirtoberemoved")
	require.NoError(t, err)
	err = testee.Dir("etc/dirA/subdirtoberemoved/nesteddir", "", dirAttrs)
	require.NoError(t, err)
	err = testee.Remove("etc/dirA/subdirtoberemoved")
	require.NoError(t, err)
	_, err = testee.File("etc/filetoberemoved", fileA)
	require.NoError(t, err)
	err = testee.Dir("etc/dirtoberemovedandrewritten", "", dirAttrs)
	require.NoError(t, err)
	err = testee.Remove("etc/dirtoberemovedandrewritten")
	require.NoError(t, err)
	err = testee.Dir("etc/dirtoberemovedandrewritten", "", dirAttrs)
	require.NoError(t, err)
	err = testee.Remove("etc/filetoberemoved")
	require.NoError(t, err)
	_, err = testee.File("etc/symlink-rel/dirA1/filetoberemoved", fileA)
	require.NoError(t, err)
	err = testee.Remove("etc/symlink-rel/dirA1/filetoberemoved")
	require.NoError(t, err)
	err = testee.Symlink("etc/symlink-toberemoved", symlinkAttrs)
	require.NoError(t, err)
	err = testee.Remove("etc/symlink-toberemoved")
	require.NoError(t, err)
	err = testee.Symlink("etc/link-toberemoved", symlinkAttrs)
	require.NoError(t, err)
	err = testee.Remove("etc/link-toberemoved")
	require.NoError(t, err)

	// Test file system boundaries
	_, err = testee.File("/../filepathsanitized", fileA)
	require.NoError(t, err)

	symlinkAttrs.Symlink = filepath.Join("..", "..")
	err = testee.Symlink("etc/symlink-sanitized", symlinkAttrs)
	if err != nil {
		t.Errorf("should not return error when symlinking outside rootfs boundaries (/..): %s", err)
		t.FailNow()
	}
	return
}

func AssertFsState(t *testing.T, rootfs, rootPath string, keywords []mtree.Keyword, expectedDirectoryHierarchy string) {
	expectedDirectoryHierarchy = strings.Replace(expectedDirectoryHierarchy, "$ROOT", rootPath, -1)
	expectedDh, err := mtree.ParseSpec(strings.NewReader(expectedDirectoryHierarchy))
	require.NoError(t, err)
	dh, err := mtree.Walk(rootfs, nil, keywords, fseval.DefaultFsEval)
	require.NoError(t, err)
	diff, err := mtree.Compare(expectedDh, dh, keywords)
	require.NoError(t, err)
	// TODO: do not exclude device files from test but map mocked device files properly back in rootless mode
	if len(diff) > 0 /* && (diff[0].Path() != "etc/blockA" || diff[0].Type() != mtree.Modified)*/ {
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
