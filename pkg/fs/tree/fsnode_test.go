package tree

import (
	"bytes"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/mgoltzsche/cntnr/pkg/fs"
	"github.com/mgoltzsche/cntnr/pkg/fs/source"
	"github.com/mgoltzsche/cntnr/pkg/fs/testutils"
	"github.com/mgoltzsche/cntnr/pkg/idutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	testErr error
)

func TestFsNode(t *testing.T) {
	tt := fsNodeTester{t, newFsNodeTree(t, true)}

	// Test node tree

	mockWriter := testutils.NewWriterMock(t, fs.AttrsHash)
	err := tt.node.Write(mockWriter)
	require.NoError(t, err)
	if !assert.Equal(t, expectedNodeOps(), mockWriter.Written, "node tree construction") {
		t.FailNow()
	}

	// Test to/from string conversion

	tt.node = newFsNodeTree(t, true)
	if !assert.Equal(t, "/ type=dir usr=1:1 mode=750 mtime=1516669302", tt.node.String(), "String()") {
		t.FailNow()
	}

	var buf bytes.Buffer
	err = tt.FS().WriteTo(&buf, fs.AttrsAll)
	require.NoError(t, err)
	input := strings.TrimSpace(buf.String())
	expectedLines := strings.Split(input, "\n")

	parsed, err := ParseFsSpec([]byte(input))
	if err != nil {
		fmt.Println("INPUT:\n" + input)
		t.Errorf("ParseFsSpec() returned error: %s (input may be wrong)", err)
		t.FailNow()
	}
	require.NoError(t, err)
	expectedNodes := testutils.MockWrites(t, tt.node).Written
	actualNodes := testutils.MockWrites(t, parsed).Nodes
	if !assert.Equal(t, expectedNodes, actualNodes, "parsed node structure") {
		fmt.Println("EXPECTED:\n" + strings.Join(expectedNodes, "\n") + "\nINPUT:\n" + input)
		t.FailNow()
	}

	// Assert String(Parse(s)) == s
	buf.Reset()
	err = parsed.WriteTo(&buf, fs.AttrsCompare)
	require.NoError(t, err)
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if !assert.Equal(t, expectedLines, lines, "String(ParseFsSpec(s)) != s") {
		fmt.Println("INPUT:\n" + input + "\n\nOUTPUT:\n" + buf.String() + "\n")
		t.FailNow()
	}

	// Assert mockWrites(t, parsed).written should be empty
	parsed.(*FsNode).RemoveWhiteouts()
	if !assert.Equal(t, []string{}, testutils.MockWrites(t, parsed).Written, "nodes written from parsed node structure") {
		t.FailNow()
	}

	// Assert mockWrites(t, add(parsed, addFile)).written should only write changed files
	mtime, err := time.Parse(time.RFC3339, "2018-01-23T01:01:42Z")
	require.NoError(t, err)
	times := fs.FileTimes{Mtime: mtime}
	changedFile := testutils.NewSourceMock(fs.TypeFile, fs.FileAttrs{Mode: 0755, UserIds: idutils.UserIds{5000, 5000}, Size: 546868, FileTimes: times}, "sha256:newhex")
	addNodes := func(f fs.FsNode) {
		added, err := f.AddUpper("/etc/addedFile", changedFile)
		require.NoError(t, err)
		require.NotNil(t, added)
		_, err = f.AddUpper("/etc/addedDir", source.NewSourceDir(fs.FileAttrs{Mode: os.ModeDir | 0754}))
		require.NoError(t, err)
		_, err = f.AddUpper("/etc/addedLink", changedFile)
		require.NoError(t, err)
		_, err = f.AddUpper("/etc/xnewdir/fifo", source.NewSourceFifo(fs.DeviceAttrs{fs.FileAttrs{Mode: 0644, UserIds: idutils.UserIds{0, 33}, FileTimes: times}, 0, 0}))
		require.NoError(t, err)
		_, err = f.AddWhiteout("/etc/symlink1/lfile1")
		require.NoError(t, err)
		existNode, err := f.Node("/etc/file1")
		require.NoError(t, err)
		existNode.Remove()
		existNode, err = f.Node("/etc/dir2")
		require.NoError(t, err)
		existNode.Remove()
		// Replace lower link
		_, err = f.AddUpper("/etc/link3", source.NewSourceFifo(fs.DeviceAttrs{fs.FileAttrs{Mode: 0644, UserIds: idutils.UserIds{0, 33}, FileTimes: times}, 0, 0}))
		require.NoError(t, err)
	}
	addNodes(parsed)
	expectedOps := []string{
		"/ type=dir usr=1:1 mode=750",
		"/etc type=dir mode=755",
		"/etc/addedDir type=dir mode=754",
		"/etc/addedFile type=file usr=5000:5000 mode=755 size=546868 hash=sha256:newhex",
		"/etc/addedLink hlink=/etc/addedFile",
		"/etc/link3 type=fifo usr=0:33 mode=644",
		"/etc/xdest type=dir usr=0:33 mode=755",
		"/etc/xdest/lfile1 type=whiteout",
		"/etc/xnewdir type=dir mode=755",
		"/etc/xnewdir/fifo type=fifo usr=0:33 mode=644",
	}
	if !assert.Equal(t, expectedOps, testutils.MockWrites(t, parsed).Written, "nodes written after changes applied to parsed node structure") {
		t.FailNow()
	}

	// Assert parsed.Diff(fs) is empty
	node := newFsNodeTree(t, false)
	node.RemoveWhiteouts()
	buf.Reset()
	err = node.WriteTo(&buf, fs.AttrsAll)
	require.NoError(t, err)
	nodeStr := buf.String()
	expectedLines = strings.Split(nodeStr, "\n")
	parsed, err = ParseFsSpec([]byte(nodeStr))
	require.NoError(t, err)
	diff, err := parsed.Diff(node)
	require.NoError(t, err)
	if !assert.Equal(t, []string{}, testutils.MockWrites(t, diff).Written, "a.Diff(a) should be empty") {
		t.FailNow()
	}
	// Assert fs.Diff(fs) has empty string representation
	buf.Reset()
	err = diff.WriteTo(&buf, fs.AttrsCompare)
	require.NoError(t, err)
	if !assert.Equal(t, []string{". type=dir", ""}, strings.Split(buf.String(), "\n"), "string(fs.Diff(fs))") {
		t.FailNow()
	}
	// Assert parsed.Diff(changedParsed) == changes
	expectedOps = append(expectedOps,
		// files that don't exist in file system b
		"/etc/file1 type=whiteout",
		"/etc/dir2 type=whiteout",
	)
	sort.Strings(expectedOps)
	changedParsed, err := ParseFsSpec([]byte(nodeStr))
	require.NoError(t, err)
	addNodes(changedParsed)
	changes, err := parsed.Diff(changedParsed)
	require.NoError(t, err)
	if !assert.Equal(t, expectedOps, testutils.MockWrites(t, changes).Written, "diff of nodes written after changes applied to parsed node structure") {
		t.FailNow()
	}
	// Assert parsed.Diff(otherFS) == change
	addNodes(node)
	// Node that equals existing should not be included in diff
	_, err = node.AddUpper("/etc/dir1", source.NewSourceDir(fs.FileAttrs{Mode: os.ModeDir | 0755, UserIds: idutils.UserIds{0, 33}, FileTimes: times}))
	require.NoError(t, err)
	// Hardlink to unchanged file and to existing hardlink
	oldFile := testutils.NewSourceMock(fs.TypeFile, fs.FileAttrs{Mode: 0644, UserIds: idutils.UserIds{1, 1}, Size: 689876, FileTimes: times}, "sha256:hex2")
	oldFile2 := *oldFile
	_, err = node.AddUpper("/etc/file2", &oldFile2)
	require.NoError(t, err)
	_, err = node.AddUpper("/etc/link1", oldFile)
	require.NoError(t, err)
	_, err = node.AddUpper("/etc/link2", oldFile)
	require.NoError(t, err)
	diff, err = parsed.Diff(node)
	require.NoError(t, err)
	if !assert.Equal(t, expectedOps, testutils.MockWrites(t, diff).Written, "parsed.Diff(otherFS)") {
		t.FailNow()
	}
	_, err = node.AddUpper("/etc/xnewlinktooldfile", oldFile)
	require.NoError(t, err)
	diff, err = parsed.Diff(node)
	require.NoError(t, err)
	expectedOps = append(expectedOps,
		// implicitly added lower file to layer to preserve hardlink in a compatible way
		"/etc/link1 type=file usr=1:1 mode=644 size=689876 hash=sha256:hex2",
		"/etc/link2 hlink=/etc/link1",
		"/etc/xnewlinktooldfile hlink=/etc/link1",
	)
	sort.Strings(expectedOps)
	if !assert.Equal(t, expectedOps, testutils.MockWrites(t, diff).Written, "parsed.Diff(otherFsWithLinkToUnchangedFiles)") {
		t.FailNow()
	}

	// Test Hash()

	// Hash() from new fs
	hash1, err := tt.FS().Hash(fs.AttrsHash)
	require.NoError(t, err)
	tt.node = newFsNodeTree(t, true)
	hash2, err := tt.FS().Hash(fs.AttrsHash)
	require.NoError(t, err)
	if hash1 != hash2 {
		t.Errorf("Hash(): same content should result in same hash")
		t.FailNow()
	}
	tt.add(changedFile, "/etc/file2")
	hash2, err = tt.FS().Hash(fs.AttrsHash)
	require.NoError(t, err)
	if hash1 == hash2 {
		t.Errorf("Hash(): should change when contents changed")
		t.FailNow()
	}
	tt.node = newFsNodeTree(t, true)

	// Hash() of two separate but equal file system diffs
	parsed, err = ParseFsSpec([]byte(nodeStr))
	require.NoError(t, err)
	changedA, err := ParseFsSpec([]byte(nodeStr))
	require.NoError(t, err)
	changedB, err := ParseFsSpec([]byte(nodeStr))
	require.NoError(t, err)
	addNodes(changedA)
	addNodes(changedB)
	diffA, err := parsed.Diff(changedA)
	require.NoError(t, err)
	diffB, err := parsed.Diff(changedB)
	require.NoError(t, err)
	hashA, err := diffA.Hash(fs.AttrsHash)
	require.NoError(t, err)
	hashB, err := diffB.Hash(fs.AttrsHash)
	require.NoError(t, err)
	if hashA != hashB {
		t.Errorf("diffA.Hash() != diffB.Hash()")
		t.FailNow()
	}

	//
	// TEST ERROR HANDLING
	//

	testErr = errors.New("expected error")
	defer func() {
		testErr = nil
	}()

	// Test WriteTo() returns error
	src := testutils.NewSourceMock(fs.TypeDir, fs.FileAttrs{Mode: 0644}, "")
	src.Err = testErr
	tt.add(src, "addedbroken")
	err = tt.FS().WriteTo(&buf, fs.AttrsAll)
	require.Error(t, err)

	// Test Hash() returns error
	_, err = tt.FS().Hash(fs.AttrsAll)
	require.Error(t, err)

	// Test Write() returns error
	err = tt.node.Write(mockWriter)
	require.Error(t, err)
}

func newFsNodeTree(t *testing.T, withOverlay bool) *FsNode {
	mtime, err := time.Parse(time.RFC3339, "2018-01-23T01:01:42Z")
	require.NoError(t, err)
	times := fs.FileTimes{Mtime: mtime}
	usr1 := &idutils.UserIds{0, 33}
	usr2 := &idutils.UserIds{1, 1}
	srcDir1 := testutils.NewSourceMock(fs.TypeDir, fs.FileAttrs{Mode: os.ModeDir | 0755, UserIds: *usr1, FileTimes: times}, "")
	newSrcFile1 := func() fs.Source {
		return testutils.NewSourceMock(fs.TypeFile, fs.FileAttrs{Mode: 0755, UserIds: *usr1, Size: 12345, FileTimes: times}, "sha256:hex1")
	}
	srcDir2 := testutils.NewSourceMock(fs.TypeDir, fs.FileAttrs{Mode: os.ModeDir | 0750, UserIds: *usr2, FileTimes: times}, "")
	srcDir3 := *srcDir2
	srcDir3.Xattrs = map[string]string{"k": "v"}
	newSrcFile2 := func() fs.Source {
		return testutils.NewSourceMock(fs.TypeFile, fs.FileAttrs{Mode: 0644, UserIds: *usr2, Size: 689876, FileTimes: times}, "sha256:hex2")
	}
	srcSymlink1 := testutils.NewSourceMock(fs.TypeSymlink, fs.FileAttrs{Symlink: "xdest", UserIds: *usr1, FileTimes: times}, "")
	srcSymlink2 := testutils.NewSourceMock(fs.TypeSymlink, fs.FileAttrs{Symlink: "../etc/xdest", UserIds: *usr2, FileTimes: times}, "")
	srcSymlink3 := testutils.NewSourceMock(fs.TypeSymlink, fs.FileAttrs{Symlink: "/etc/xnewdest/newdir", UserIds: *usr1, FileTimes: times}, "")
	srcLink := newSrcFile2()
	srcArchive1 := &testutils.SourceOverlayMock{testutils.NewSourceMock(fs.TypeOverlay, fs.FileAttrs{UserIds: *usr1, Size: 98765, FileTimes: times}, "sha256:hex3")}
	srcArchive2 := &testutils.SourceOverlayMock{testutils.NewSourceMock(fs.TypeOverlay, fs.FileAttrs{UserIds: *usr1, Size: 87658, FileTimes: times}, "sha256:hex4")}
	tt := fsNodeTester{t, NewFS()}
	tt.add(srcDir1, "")
	tt.add(srcDir1, "/emptydir")
	tt.add(srcDir1, "/root/empty dir")
	tt.add(srcDir2, "")
	tt.add(srcDir2, ".")
	tt.add(srcDir2, "/")
	xdest := tt.add(srcDir1, "/etc/xdest")
	tt.add(srcDir1, "/etc/xdest/overridewithfile")
	tt.add(newSrcFile1(), "/etc/xdest/overridewithdir")
	tt.add(newSrcFile2(), "/etc/file2")
	tt.add(srcSymlink2, "/etc/symlink2")
	tt.add(srcDir2, "/etc/dir1")
	tt.add(srcDir1, "/etc/dir1").
		add(&srcDir3, "../dir2/").
		add(&srcDir3, "../../etc/dir2/")
	srcFile1 := newSrcFile1()
	tt.add(srcFile1, "/etc/file1")
	srcFile2 := newSrcFile2()
	tt.add(srcFile2, "/etc/dir2/x/y/filem")

	// Test symlinks
	tt.add(srcSymlink2, "/etc/symlink2")
	tt.add(srcSymlink3, "/etc/symlink3")
	symlink := tt.add(srcSymlink1, "/etc/symlink1")
	tt.add(newSrcFile1(), "/etc/symlink1/lfile1")
	tt.add(newSrcFile1(), "/etc/symlink1/overridewithfile")
	tt.add(srcDir1, "/etc/symlink1/overridewithdir")
	tt.add(srcDir1, "/etc/symlink1/ldir1")
	srcFile1ResolvedParent := newSrcFile1()
	tt.add(srcFile1ResolvedParent, "/etc/symlink2/lfile2")
	tt.add(newSrcFile1(), "/etc/symlink3/lfile3")

	// Test link
	tt.add(srcLink, "/etc/link1")
	tt.add(srcLink, "/etc/link2")
	tt.add(srcLink, "/etc/link3")
	tt.add(srcLink, "/etc/linkreplacewithparentdir")
	tt.add(newSrcFile1(), "/etc/linkreplacewithparentdir/lfile3")
	tt.add(srcLink, "/etc/linkreplacewithdir")
	tt.add(srcDir1, "/etc/linkreplacewithdir")
	tt.add(srcLink, "/etc/linkreplacewithfile")
	tt.add(newSrcFile1(), "/etc/linkreplacewithfile")

	// Test node overwrites
	tt.add(newSrcFile1(), "/etc/fileoverwrite")
	tt.add(newSrcFile2(), "/etc/fileoverwrite")
	tt.add(newSrcFile1(), "/etc/fileoverwriteimplicit")
	tt.add(newSrcFile2(), "/etc/fileoverwriteimplicit/filex")
	tt.add(newSrcFile1(), "/etc/diroverwrite1/file")
	tt.add(srcDir2, "/etc/diroverwrite1")
	tt.add(newSrcFile1(), "/etc/diroverwrite2/file")
	tt.add(newSrcFile2(), "/etc/diroverwrite2")
	tt.add(srcSymlink1, "/etc/symlinkoverwritefile")
	tt.add(newSrcFile1(), "/etc/symlinkoverwritefile")
	tt.add(srcSymlink1, "/etc/symlinkoverwritedir")
	tt.add(srcDir1, "/etc/symlinkoverwritedir")
	// Test whiteout
	tt.add(newSrcFile1(), "/etc/filetobeoverwrittenbywhiteout")
	wh, err := tt.node.AddWhiteout("/etc/filetobeoverwrittenbywhiteout")
	require.NoError(t, err)
	assert.NotNil(t, wh)
	tt.add(srcDir1, "/etc/dirtobeoverwrittenbywhiteout")
	tt.add(newSrcFile1(), "/etc/dirtobeoverwrittenbywhiteout/nestedToBeDel")
	wh, err = tt.node.AddWhiteout("/etc/dirtobeoverwrittenbywhiteout")
	require.NoError(t, err)
	assert.NotNil(t, wh)
	tt.add(newSrcFile1(), "/etc/dircontainingwhiteout/whiteoutfile")
	wh, err = tt.node.AddWhiteout("/etc/dircontainingwhiteout/whiteoutfile")
	require.NoError(t, err)
	assert.NotNil(t, wh)
	// Test remove
	rmDir := tt.add(srcDir1, "/etc/dirtoberemoved")
	tt.add(newSrcFile1(), "/etc/dirtoberemoved/nestedfiletoberemoved")
	rmFile := tt.add(newSrcFile1(), "/etc/filetoberemoved")
	tt.add(srcDir1, "/etc/parentrmdir")
	rmChild := tt.add(srcDir1, "/etc/parentrmdir/1stchildtoberemoved")
	rmDir.node.Remove()
	rmFile.node.Remove()
	rmChild.node.Remove()

	// Test overlay
	if withOverlay {
		tt.add(newSrcFile1(), "/overlay1/dir1/file1")
		tt.add(srcArchive1, "/overlay1")
		// /overlay1 dir permissions should be set after archive has been extracted
		tt.add(srcDir2, "/overlay1")
		tt.add(srcDir2, "/overlay1/dir2")
		tt.add(newSrcFile1(), "/overlay3")
		tt.add(srcArchive1, "/overlay3")
		tt.add(srcArchive1, "/overlay4")
		tt.add(srcArchive2, "/overlay4")
		// /overlay2 dir should not be added as noop source with parent's attributes
		tt.add(srcArchive1, "/overlay2")
		tt.add(srcDir2, "/overlay2/dir2")
		tt.add(srcArchive1, "/overlayx")
		tt.add(srcDir1, "/overlayx")
		overlay := tt.add(srcArchive1, "/overlayx/dirx/nestedoverlay")
		tt.add(newSrcFile1(), "/overlayx/dirx/nestedoverlay/nestedoverlaychild")

		// Test path resolution
		xdest.add(newSrcFile1(), "../../etc/xadd-resolve-rel")
		symlink.add(newSrcFile1(), "../../etc/xadd-resolve-rel-link")
		xdest.add(newSrcFile1(), "/etc/xadd-resolve-abs")
		xdest.add(newSrcFile2(), "../xadd-resolve-parent")

		// Test path resolution
		tt.assertResolve("/etc/file1", "/etc/file1", srcFile1, true)
		tt.assertResolve("etc/file1", "/etc/file1", srcFile1, true)
		etc := tt.assertResolve("etc", "/etc", nil, true)
		_, ok := etc.node.source.(*source.SourceDir)
		assert.True(t, ok, "/etc should be sourcedir")
		tt.assertResolve("./etc", "/etc", nil, true)
		etc.assertResolve("dir2/x/y/filem", "/etc/dir2/x/y/filem", srcFile2, true)
		etc.assertResolve(".", "/etc", nil, true)
		etc.assertResolve("../etc/file1", "/etc/file1", srcFile1, true)
		etc.assertResolve("..", "/", srcDir2, true)
		etc.assertResolve("/", "/", srcDir2, true)
		tt.assertResolve("/", "/", srcDir2, true)
		etc.assertResolve("../..", "/", nil, false)
		tt.assertResolve("../etc", "/etc", nil, false)
		tt.assertResolve("/etc/symlink2/lfile2", "/etc/xdest/lfile2", srcFile1ResolvedParent, true)
		tt.assertResolve("/etc/symlink2/lfile2/nonexisting", "", nil, false)
		tt.assertResolve("nonexisting", "", nil, false)
		overlay1 := tt.assertResolve("/overlay1", "/overlay1", nil, true)
		_, ok = overlay1.node.source.(*source.SourceDir)
		assert.True(t, ok, "/overlay1 should be sourcedir")
		// parent resolution
		etc.assertResolve("..", "/", srcDir2, true)
		// ...within overlay
		overlay.assertResolve("..", "/overlayx/dirx", srcParentDir, true).
			assertResolve("..", "/overlayx", nil, true).
			assertResolve("..", "/", srcDir2, true)
	}
	return tt.node
}

func expectedNodeOps() []string {
	expected := `
		/ type=dir usr=1:1 mode=750
		/emptydir type=dir usr=0:33 mode=755
		/etc type=dir mode=755
		/etc/dir1 type=dir usr=0:33 mode=755
		/etc/dir2 type=dir usr=1:1 mode=750 xattr.k=v
		/etc/dir2/x type=dir mode=755
		/etc/dir2/x/y type=dir mode=755
		/etc/dir2/x/y/filem type=file usr=1:1 mode=644 size=689876 hash=sha256:hex2
		/etc/dircontainingwhiteout type=dir mode=755
		/etc/dircontainingwhiteout/whiteoutfile type=whiteout
		/etc/diroverwrite1 type=dir usr=1:1 mode=750
		/etc/diroverwrite1/file type=file usr=0:33 mode=755 size=12345 hash=sha256:hex1
		/etc/diroverwrite2 type=file usr=1:1 mode=644 size=689876 hash=sha256:hex2
		/etc/dirtobeoverwrittenbywhiteout type=whiteout
		/etc/file1 type=file usr=0:33 mode=755 size=12345 hash=sha256:hex1
		/etc/file2 type=file usr=1:1 mode=644 size=689876 hash=sha256:hex2
		/etc/fileoverwrite type=file usr=1:1 mode=644 size=689876 hash=sha256:hex2
		/etc/fileoverwriteimplicit type=dir mode=755
		/etc/fileoverwriteimplicit/filex type=file usr=1:1 mode=644 size=689876 hash=sha256:hex2
		/etc/filetobeoverwrittenbywhiteout type=whiteout
		/etc/link1 type=file usr=1:1 mode=644 size=689876 hash=sha256:hex2
		/etc/link2 hlink=/etc/link1
		/etc/link3 hlink=/etc/link1
		/etc/linkreplacewithdir type=dir usr=0:33 mode=755
		/etc/linkreplacewithfile type=file usr=0:33 mode=755 size=12345 hash=sha256:hex1
		/etc/linkreplacewithparentdir type=dir mode=755
		/etc/linkreplacewithparentdir/lfile3 type=file usr=0:33 mode=755 size=12345 hash=sha256:hex1
		/etc/parentrmdir type=dir usr=0:33 mode=755
		/etc/symlink1 type=symlink usr=0:33 link=xdest
		/etc/symlink2 type=symlink usr=1:1 link=../etc/xdest
		/etc/symlink3 type=symlink usr=0:33 link=/etc/xnewdest/newdir
		/etc/symlinkoverwritedir type=dir usr=0:33 mode=755
		/etc/symlinkoverwritefile type=file usr=0:33 mode=755 size=12345 hash=sha256:hex1
		/etc/xadd-resolve-abs type=file usr=0:33 mode=755 size=12345 hash=sha256:hex1
		/etc/xadd-resolve-parent type=file usr=1:1 mode=644 size=689876 hash=sha256:hex2
		/etc/xadd-resolve-rel type=file usr=0:33 mode=755 size=12345 hash=sha256:hex1
		/etc/xadd-resolve-rel-link type=file usr=0:33 mode=755 size=12345 hash=sha256:hex1
		/etc/xdest type=dir usr=0:33 mode=755
		/etc/xdest/ldir1 type=dir usr=0:33 mode=755
		/etc/xdest/lfile1 type=file usr=0:33 mode=755 size=12345 hash=sha256:hex1
		/etc/xdest/lfile2 type=file usr=0:33 mode=755 size=12345 hash=sha256:hex1
		/etc/xdest/overridewithdir type=dir usr=0:33 mode=755
		/etc/xdest/overridewithfile type=file usr=0:33 mode=755 size=12345 hash=sha256:hex1
		/etc/xnewdest type=dir mode=755
		/etc/xnewdest/newdir type=dir mode=755
		/etc/xnewdest/newdir/lfile3 type=file usr=0:33 mode=755 size=12345 hash=sha256:hex1
		/overlay1 type=dir mode=755
		/overlay1/dir1 type=dir mode=755
		/overlay1/dir1/file1 type=file usr=0:33 mode=755 size=12345 hash=sha256:hex1
		/overlay1 type=overlay usr=0:33 size=98765 hash=sha256:hex3
		/overlay1 type=dir usr=1:1 mode=750
		/overlay1/dir2 type=dir usr=1:1 mode=750
		/overlay2 type=dir mode=755
		/overlay2 type=overlay usr=0:33 size=98765 hash=sha256:hex3
		/overlay2/dir2 type=dir usr=1:1 mode=750
		/overlay3 type=dir mode=755
		/overlay3 type=overlay usr=0:33 size=98765 hash=sha256:hex3
		/overlay4 type=dir mode=755
		/overlay4 type=overlay usr=0:33 size=98765 hash=sha256:hex3
		/overlay4 type=overlay usr=0:33 size=87658 hash=sha256:hex4
		/overlayx type=dir mode=755
		/overlayx type=overlay usr=0:33 size=98765 hash=sha256:hex3
		/overlayx type=dir usr=0:33 mode=755
		/overlayx/dirx/nestedoverlay type=dir mode=755
		/overlayx/dirx/nestedoverlay type=overlay usr=0:33 size=98765 hash=sha256:hex3
		/overlayx/dirx/nestedoverlay/nestedoverlaychild type=file usr=0:33 mode=755 size=12345 hash=sha256:hex1
		/root type=dir mode=755
		/root/empty%20dir type=dir usr=0:33 mode=755
	`
	expectedLines := strings.Split(strings.TrimSpace(expected), "\n")
	for i, line := range expectedLines {
		expectedLines[i] = strings.TrimSpace(line)
	}
	return expectedLines
}

func treePaths(node *FsNode, m map[string]bool) {
	if node.NodeType != fs.TypeWhiteout {
		m[node.Path()] = true
	}
	if node.child != nil {
		treePaths(node.child, m)
	}
	if node.next != nil {
		treePaths(node.next, m)
	}
}

type fsNodeTester struct {
	t    *testing.T
	node *FsNode
}

func (s *fsNodeTester) FS() *FsNode {
	return s.node
}

func (s *fsNodeTester) add(src fs.Source, dest string) *fsNodeTester {
	f, err := s.node.addUpper(dest, src)
	require.NoError(s.t, err)
	require.NotNil(s.t, f)
	return &fsNodeTester{s.t, f}
}

func (s *fsNodeTester) assertResolve(path string, expectedPath string, expectedSrc fs.Source, valid bool) *fsNodeTester {
	node, err := s.node.node(path)
	if err != nil {
		if !valid {
			return nil
		}
		s.t.Errorf("resolve path %s: %s", path, err)
		s.t.FailNow()
	} else if !valid {
		s.t.Errorf("path %s should yield error but returned node %s", path, node.Path())
		s.t.FailNow()
	}
	nPath := node.Path()
	if nPath != expectedPath {
		s.t.Errorf("node %s path %s should resolve to %s but was %q", node.Path(), path, expectedPath, nPath)
		s.t.FailNow()
	}
	if expectedSrc != nil {
		eq, err := node.source.Equal(expectedSrc)
		require.NoError(s.t, err)
		if !eq {
			a := node.source.Attrs()
			s.t.Errorf("unexpected source {%s} at %s", (&a).AttrString(fs.AttrsAll), nPath)
			s.t.FailNow()
		}
	}
	return &fsNodeTester{s.t, node}
}

func expectedWriteOps(t *testing.T) []string {
	expectedOps := []string{}
	typeRegex := regexp.MustCompile(" type=([^ ]+)")
	exclAttrRegex := regexp.MustCompile(" hlink=[^ ]+| size=[^ ]+| hash=[^ ]+|\\.[^ =]+=[^ ]+")
	for _, line := range expectedNodeOps() {
		line := strings.TrimSpace(line)
		path := line[:strings.Index(line, " type=")]
		attrs := line[len(path):]
		path, err := url.PathUnescape(path)
		require.NoError(t, err)
		var t fs.NodeType
		if m := typeRegex.FindStringSubmatch(line); len(m) > 0 {
			t = fs.NodeType(m[1])
		}
		if t == fs.TypeOverlay {
			line = filepath.Join(path, "xtracted") + " type=file usr=0:33 mode=644"
		} else if pos := strings.Index(line, " hlink="); pos != -1 {
			line = path + " type=link" + line[pos:]
		} else if t == fs.TypeWhiteout {
			line = path + " type=whiteout"
		} else {
			line = path + string(exclAttrRegex.ReplaceAll([]byte(attrs), []byte("")))
		}
		expectedOps = append(expectedOps, line)
	}
	return expectedOps
}
