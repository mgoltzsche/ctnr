package files

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mgoltzsche/cntnr/pkg/idutils"
	"github.com/stretchr/testify/require"
)

var (
	testErr error
)

func TestFsNode(t *testing.T) {
	usr1 := &idutils.UserIds{0, 33}
	usr2 := &idutils.UserIds{1, 1}
	srcDir1 := &sourceMock{TypeDir, FileAttrs{Mode: os.ModeDir | 0755, UserIds: *usr1}, ""}
	srcFile1 := &sourceMock{TypeFile, FileAttrs{Mode: 0755, Size: 12345, UserIds: *usr1}, ""}
	srcDir2 := &sourceMock{TypeDir, FileAttrs{Mode: os.ModeDir | 0750, UserIds: *usr2}, ""}
	srcDir3 := *srcDir2
	srcDir3.Xattrs = []XAttr{{"k", []byte("v")}}
	srcFile2 := &sourceMock{TypeFile, FileAttrs{Mode: 0644, Size: 689876, UserIds: *usr2}, "file:sha256:hex"}
	srcSymlink1 := &sourceMock{TypeSymlink, FileAttrs{Mode: os.ModeSymlink | 0777, Link: "xdest", UserIds: *usr1}, ""}
	srcSymlink2 := &sourceMock{TypeSymlink, FileAttrs{Mode: os.ModeSymlink | 0777, Link: "../etc/xdest", UserIds: *usr2}, ""}
	srcSymlink3 := &sourceMock{TypeSymlink, FileAttrs{Mode: os.ModeSymlink | 0777, Link: "/etc/xnewdest/newdir", UserIds: *usr1}, ""}
	srcLink := NewSourceLink(srcFile2)
	srcArchive := &sourceOverlayMock{sourceMock{TypeOverlay, FileAttrs{Mode: os.ModeDir | 0755, UserIds: *usr1, Size: 98765}, "archive:sha256:hex"}}
	tt := fsNodeTester{t, nil}
	newTestee := func() {
		tt.testee = NewFS()
		tt.add(nil, "")
		tt.add(nil, "/emptydir")
		tt.add(nil, "/root/emptydir")
		tt.add(srcDir2, "")
		xdest := tt.add(srcDir1, "/etc/xdest")
		tt.add(srcDir1, "/etc/xdest/overridewithfile")
		tt.add(srcFile1, "/etc/xdest/overridewithdir")
		tt.add(srcFile2, "/etc/file2")
		tt.add(srcSymlink2, "/etc/symlink2")
		tt.add(srcDir2, "/etc/dir1")
		tt.add(srcDir1, "/etc/dir1")
		tt.add(&srcDir3, "/etc/dir2/")
		tt.add(srcFile1, "/etc/file1")
		tt.add(srcFile2, "/etc/dir2/x/y/filem")

		// Test symlinks
		tt.add(srcSymlink2, "/etc/symlink2")
		tt.add(srcSymlink3, "/etc/symlink3")
		symlink := tt.add(srcSymlink1, "/etc/symlink1")
		tt.add(srcFile1, "/etc/symlink1/lfile1")
		tt.add(srcFile1, "/etc/symlink1/overridewithfile")
		tt.add(srcDir1, "/etc/symlink1/overridewithdir")
		tt.add(srcDir1, "/etc/symlink1/ldir1")
		tt.add(srcFile1, "/etc/symlink2/lfile2")
		tt.add(srcFile1, "/etc/symlink3/lfile3")

		// Test link
		tt.add(srcLink, "/etc/link1")
		tt.add(srcLink, "/etc/link2")
		tt.add(srcLink, "/etc/linkreplacewithparentdir")
		tt.add(srcFile1, "/etc/linkreplacewithparentdir/lfile3")
		tt.add(srcLink, "/etc/linkreplacewithdir")
		tt.add(srcDir1, "/etc/linkreplacewithdir")
		tt.add(srcLink, "/etc/linkreplacewithfile")
		tt.add(srcFile1, "/etc/linkreplacewithfile")

		// Test node overwrites
		tt.add(srcFile1, "/etc/fileoverwrite")
		tt.add(srcFile2, "/etc/fileoverwrite")
		tt.add(srcFile1, "/etc/fileoverwriteimplicit")
		tt.add(srcFile2, "/etc/fileoverwriteimplicit/filex")
		tt.add(srcFile1, "/etc/diroverwrite1/file")
		tt.add(srcDir1, "/etc/diroverwrite1")
		tt.add(srcFile1, "/etc/diroverwrite2/file")
		tt.add(srcFile2, "/etc/diroverwrite2")
		tt.add(srcSymlink1, "/etc/symlinkoverwritefile")
		tt.add(srcFile1, "/etc/symlinkoverwritefile")
		tt.add(srcSymlink1, "/etc/symlinkoverwritedir")
		tt.add(srcDir1, "/etc/symlinkoverwritedir")

		// Test overlay
		tt.add(srcFile1, "/overlay1/dir1/file1")
		tt.add(srcArchive, "/overlay1")
		tt.add(srcDir2, "/overlay1")
		tt.add(srcDir2, "/overlay1/dir2")
		tt.add(srcFile1, "/overlay3")
		tt.add(srcArchive, "/overlay3")
		tt.add(srcArchive, "/overlay4")
		tt.add(srcArchive, "/overlay4")
		tt.add(srcArchive, "/overlay2")
		tt.add(srcDir2, "/overlay2/dir2")
		tt.add(srcArchive, "/overlayx")
		tt.add(srcArchive, "/overlayx/dirx/nestedoverlay")

		// Test path resolution
		xdest.add(srcFile1, "../../etc/xadd-resolve-rel")
		symlink.add(srcFile1, "../../etc/xadd-resolve-rel-link")
		xdest.add(srcFile1, "/etc/xadd-resolve-abs")
		xdest.add(srcFile2, "../xadd-resolve-parent")
	}
	newTestee()

	// Test path resolution
	tt.assertResolve("/etc/file1", "etc/file1", srcFile1, true)
	tt.assertResolve("etc/file1", "etc/file1", srcFile1, true)
	etc := tt.assertResolve("etc", "etc", srcDirImplicit, true)
	tt.assertResolve("./etc", "etc", srcDirImplicit, true)
	etc.assertResolve("dir2/x/y/filem", "etc/dir2/x/y/filem", srcFile2, true)
	etc.assertResolve(".", "etc", srcDirImplicit, true)
	etc.assertResolve("../etc/file1", "etc/file1", srcFile1, true)
	etc.assertResolve("..", ".", nil, true)
	etc.assertResolve("../..", ".", nil, false)
	tt.assertResolve("../etc", "etc", srcDirImplicit, false)
	tt.assertResolve("/etc/symlink2/lfile2", "etc/xdest/lfile2", srcFile1, true)
	tt.assertResolve("/etc/symlink2/lfile2/nonexisting", "", nil, false)
	tt.assertResolve("nonexisting", "", nil, false)

	// Test string representation

	var buf bytes.Buffer
	err := tt.testee.WriteTo(&buf)
	require.NoError(t, err)
	lines := strings.Split(strings.Trim(buf.String(), "\n"), "\n")
	expected := `
		drwxr-x--- . usr=1:1
		drwxr-xr-x emptydir usr=$USR
		drwxr-xr-x etc/dir1 usr=0:33
		drwxr-x--- etc/dir2 usr=1:1 xattrs="k"=76
		-rw-r--r-- etc/dir2/x/y/filem usr=1:1 size=689876 hash=file:sha256:hex
		drwxr-xr-x etc/diroverwrite1 usr=0:33
		-rwxr-xr-x etc/diroverwrite1/file usr=0:33 size=12345
		-rw-r--r-- etc/diroverwrite2 usr=1:1 size=689876 hash=file:sha256:hex
		-rwxr-xr-x etc/file1 usr=0:33 size=12345
		-rw-r--r-- etc/file2 usr=1:1 size=689876 hash=file:sha256:hex
		-rw-r--r-- etc/fileoverwrite usr=1:1 size=689876 hash=file:sha256:hex
		-rw-r--r-- etc/fileoverwriteimplicit/filex usr=1:1 size=689876 hash=file:sha256:hex
		-rw-r--r-- etc/link1 usr=1:1 size=689876 hash=file:sha256:hex
		-rw-r--r-- etc/link2 usr=1:1 size=689876 hash=file:sha256:hex link="/etc/link1"
		drwxr-xr-x etc/linkreplacewithdir usr=0:33
		-rwxr-xr-x etc/linkreplacewithfile usr=0:33 size=12345
		-rwxr-xr-x etc/linkreplacewithparentdir/lfile3 usr=0:33 size=12345
		Lrwxrwxrwx etc/symlink1 usr=0:33 link="xdest"
		Lrwxrwxrwx etc/symlink2 usr=1:1 link="../etc/xdest"
		Lrwxrwxrwx etc/symlink3 usr=0:33 link="/etc/xnewdest/newdir"
		drwxr-xr-x etc/symlinkoverwritedir usr=0:33
		-rwxr-xr-x etc/symlinkoverwritefile usr=0:33 size=12345
		-rwxr-xr-x etc/xadd-resolve-abs usr=0:33 size=12345
		-rw-r--r-- etc/xadd-resolve-parent usr=1:1 size=689876 hash=file:sha256:hex
		-rwxr-xr-x etc/xadd-resolve-rel usr=0:33 size=12345
		-rwxr-xr-x etc/xadd-resolve-rel-link usr=0:33 size=12345
		drwxr-xr-x etc/xdest usr=0:33
		drwxr-xr-x etc/xdest/ldir1 usr=0:33
		-rwxr-xr-x etc/xdest/lfile1 usr=0:33 size=12345
		-rwxr-xr-x etc/xdest/lfile2 usr=0:33 size=12345
		drwxr-xr-x etc/xdest/overridewithdir usr=0:33
		-rwxr-xr-x etc/xdest/overridewithfile usr=0:33 size=12345
		-rwxr-xr-x etc/xnewdest/newdir/lfile3 usr=0:33 size=12345
		-rwxr-xr-x overlay1/dir1/file1 usr=0:33 size=12345
		drwxr-xr-x overlay1 usr=0:33 size=98765 hash=archive:sha256:hex
		drwxr-x--- overlay1 usr=1:1
		drwxr-x--- overlay1/dir2 usr=1:1
		drwxr-xr-x overlay2 usr=0:33 size=98765 hash=archive:sha256:hex
		drwxr-x--- overlay2/dir2 usr=1:1
		drwxr-xr-x overlay3 usr=0:33 size=98765 hash=archive:sha256:hex
		drwxr-xr-x overlay4 usr=0:33 size=98765 hash=archive:sha256:hex
		drwxr-xr-x overlay4 usr=0:33 size=98765 hash=archive:sha256:hex
		drwxr-xr-x overlayx usr=0:33 size=98765 hash=archive:sha256:hex
		drwxr-xr-x overlayx/dirx/nestedoverlay usr=0:33 size=98765 hash=archive:sha256:hex
		drwxr-xr-x root/emptydir usr=$USR
	`
	expected = strings.Replace(expected, "$USR", fmt.Sprintf("%d:%d", os.Geteuid(), os.Getegid()), -1)
	expectedLines := strings.Split(strings.TrimSpace(expected), "\n")
	assertEqualSlice(t, lines, expectedLines, "WriteTo()")

	// Test Hash()

	hash1, err := tt.testee.Hash()
	require.NoError(t, err)
	newTestee()
	hash2, err := tt.testee.Hash()
	require.NoError(t, err)
	if hash1 != hash2 {
		t.Errorf("Hash(): should be idempotent")
		t.FailNow()
	}
	tt.add(srcFile1, "/etc/file2")
	hash2, err = tt.testee.Hash()
	require.NoError(t, err)
	if hash1 == hash2 {
		t.Errorf("Hash(): should change when contents changed")
		t.FailNow()
	}
	newTestee()

	// Test WriteFiles()

	expectedOps := nodesToExpectedWriteOps(expected)
	writer := newWriterMock(t)
	err = tt.testee.WriteFiles(writer)
	require.NoError(t, err)
	assertEqualSlice(t, writer.written, expectedOps, "WriteFiles()")

	//
	// TEST ERROR HANDLING
	//

	testErr = errors.New("expected error")

	// Test WriteTo() returns error
	err = tt.testee.WriteTo(&buf)
	require.Error(t, err)

	// Test Hash() returns error
	_, err = tt.testee.Hash()
	require.Error(t, err)

	// Test WriteFiles() returns error
	err = tt.testee.WriteFiles(writer)
	require.Error(t, err)
}

type fsNodeTester struct {
	t      *testing.T
	testee *FsNode
}

func (s *fsNodeTester) add(src Source, dest string) *fsNodeTester {
	f, err := s.testee.Add(src, dest)
	require.NoError(s.t, err)
	require.NotNil(s.t, f)
	return &fsNodeTester{s.t, f}
}

func (s *fsNodeTester) assertResolve(path string, expectedPath string, expectedSrc Source, valid bool) *fsNodeTester {
	node, err := s.testee.Node(path)
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
		s.t.Errorf("path %s should resolve to %s but was %q", path, expectedPath, nPath)
		s.t.FailNow()
	}
	if expectedSrc != nil && node.source != expectedSrc {
		s.t.Errorf("unexpected source %+v at %s", node.source, nPath)
		s.t.FailNow()
	}
	return &fsNodeTester{s.t, node}
}

func nodesToExpectedWriteOps(fstree string) []string {
	lines := strings.Split(strings.TrimSpace(fstree), "\n")
	expectedOps := []string{}
	added := map[string]string{}
	for _, line := range lines {
		line := strings.TrimSpace(line)
		if line != "" && line[0] != '#' {
			path := strings.Split(line, " ")[1]
			t := string(line[0])
			isArchive := strings.Index(line, " hash=archive:sha256:hex") > 0
			isLink := strings.Index(line, " link=") > 0 && strings.Index(line, " hash=") > 0
			if isArchive {
				// require a file write operation for each archive
				// as the mocked archive source would write
				path = filepath.Join(path, "xtracted")
				t = "-"
			} else if isLink {
				t = "l"
			}
			addPaths(t, path, &expectedOps, added)
		}
	}
	return expectedOps
}

func addPaths(t, path string, ops *[]string, added map[string]string) {
	if added[path] == "" || t != "_" {
		// require write operation if no implicit dir or implicit dir not yet added
		added[path] = t
		if path != "." {
			addPaths("_", filepath.Dir(path), ops, added)
		}
		*ops = append(*ops, t+" "+path)
	}
}

func assertEqualSlice(t *testing.T, lines []string, expected []string, name string) {
	for i, line := range lines[0:int32(math.Min(float64(len(expected)), float64(len(lines))))] {
		expectedLine := strings.TrimSpace(expected[i])
		if expectedLine != line {
			fmt.Println("ACTUAL:\n", strings.Join(lines, "\n"), "\n\nEXPECTED:\n", strings.Join(expected, "\n")+"\n")
			t.Errorf("%s: unexpected line %d.\nexpected:\n  %s\nwas:\n  %s", name, i, expectedLine, line)
			t.FailNow()
		}
	}
	if len(expected) < len(lines) {
		t.Error("unexpected:\n  " + strings.Join(lines[len(expected):], "\n  "))
		t.FailNow()
	}
	if len(expected) > len(lines) {
		t.Error("missing:\n  " + strings.Join(expected[len(lines):], "\n  "))
		t.FailNow()
	}
}

type writerMock struct {
	t            *testing.T
	written      []string
	writtenPaths map[string]bool
}

func newWriterMock(t *testing.T) *writerMock {
	return &writerMock{t, nil, map[string]bool{}}
}

func (s *writerMock) File(path string, src io.Reader, attrs FileAttrs) error {
	require.True(s.t, attrs.Mode != 0, "%s: mode != 0", path)
	require.True(s.t, attrs.Link == "", "%s: link != ''", path)
	require.NotNil(s.t, src, "%s: source not provided", path)
	s.written = append(s.written, "- "+path)
	s.writtenPaths[filepath.Clean("/"+path)] = true
	return nil
}
func (s *writerMock) Link(path string, attrs FileAttrs) error {
	require.True(s.t, attrs.Link != "", "%s: link dest must be provided", path)
	s.written = append(s.written, "l "+path)
	s.writtenPaths[filepath.Clean("/"+path)] = true
	return nil
}
func (s *writerMock) Symlink(path string, attrs FileAttrs) error {
	require.True(s.t, attrs.Link != "", "%s: symlink dest must be provided", path)
	s.written = append(s.written, "L "+path)
	s.writtenPaths[filepath.Clean("/"+path)] = true
	return nil
}
func (s *writerMock) Dir(path string, attrs FileAttrs) error {
	require.True(s.t, attrs.Mode != 0, "%s: mode != 0", path)
	require.True(s.t, attrs.Link == "", "%s: link != ''", path)
	s.written = append(s.written, "d "+path)
	s.writtenPaths[filepath.Clean("/"+path)] = true
	return nil
}
func (s *writerMock) DirImplicit(path string, attrs FileAttrs) error {
	require.True(s.t, attrs.Mode != 0, "%s: mode != 0", path)
	require.True(s.t, attrs.Link == "", "%s: link != ''", path)
	s.written = append(s.written, "_ "+path)
	s.writtenPaths[filepath.Clean("/"+path)] = true
	return nil
}
func (s *writerMock) Fifo(path string, attrs FileAttrs) error {
	s.written = append(s.written, "F "+path)
	s.writtenPaths[filepath.Clean("/"+path)] = true
	return nil
}
func (s *writerMock) Block(path string, attrs FileAttrs) error {
	s.written = append(s.written, "b "+path)
	s.writtenPaths[filepath.Clean("/"+path)] = true
	return nil
}
func (s *writerMock) Remove(path string) error {
	s.written = append(s.written, "r "+path)
	s.writtenPaths[filepath.Clean("/"+path)] = true
	return nil
}

type sourceMock struct {
	t SourceType
	FileAttrs
	hash string
}

func (s *sourceMock) Type() SourceType      { return s.t }
func (s *sourceMock) Attrs() *FileAttrs     { return &s.FileAttrs }
func (s *sourceMock) Hash() (string, error) { return s.hash, testErr }
func (s *sourceMock) WriteFiles(dest string, w Writer) error {
	t := s.t
	if t.IsDir() {
		w.Dir(dest, s.FileAttrs)
	} else if t.IsSymlink() {
		w.Symlink(dest, s.FileAttrs)
	} else {
		w.File(dest, bytes.NewReader([]byte("mockcontent")), s.FileAttrs)
	}
	return testErr
}

type sourceOverlayMock struct {
	sourceMock
}

func (s *sourceOverlayMock) WriteFiles(dest string, w Writer) error {
	return w.File(filepath.Join(dest, "xtracted"), bytes.NewReader([]byte("content")), s.FileAttrs)
}
