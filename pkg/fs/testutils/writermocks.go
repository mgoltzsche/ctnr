package testutils

import (
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mgoltzsche/ctnr/pkg/fs"
	"github.com/stretchr/testify/require"
)

type TestNode interface {
	Write(fs.Writer) error
}

func MockWrites(t *testing.T, f TestNode) *WriterMock {
	mockWriter := NewWriterMock(t, fs.AttrsHash)
	err := f.Write(mockWriter)
	require.NoError(t, err)
	return mockWriter
}

type WriterMock struct {
	t            *testing.T
	Written      []string
	WrittenPaths map[string]bool
	Nodes        []string
	attrs        fs.AttrSet
}

func NewWriterMock(t *testing.T, attrs fs.AttrSet) *WriterMock {
	return &WriterMock{t, []string{}, map[string]bool{}, []string{}, attrs}
}
func (w *WriterMock) Parent() error {
	return nil
}
func (w *WriterMock) Mkdir(dir string) error {
	return nil
}
func (s *WriterMock) LowerNode(path, name string, a *fs.NodeAttrs) error {
	line := s.opString(a.NodeType, path, &a.FileAttrs)
	if a.Hash != "" {
		line += " hash=" + a.Hash
	}
	if a.URL != "" {
		line += " url=" + a.URL
	}
	if a.HTTPInfo != "" {
		line += " http=" + a.HTTPInfo
	}
	s.Nodes = append(s.Nodes, line)
	return nil
}
func (s *WriterMock) Lazy(path, name string, src fs.LazySource, _ map[fs.Source]string) error {
	a := src.Attrs()
	da, err := src.DeriveAttrs()
	require.NoError(s.t, err)
	require.True(s.t, a.Symlink == "", "%s: link != ''", path)
	require.NotNil(s.t, src, "%s: source not provided", path)
	line := s.opString(a.NodeType, path, &a.FileAttrs) + " hash=" + da.Hash
	s.Nodes = append(s.Nodes, line)
	s.Written = append(s.Written, line)
	s.WrittenPaths[filepath.Clean("/"+path)] = true
	return nil
}
func (s *WriterMock) File(path string, src fs.FileSource) (fs.Source, error) {
	require.NotNil(s.t, src, "%s: source not provided", path)
	a := src.Attrs()
	require.True(s.t, a.Mode != 0 || strings.Contains(path, "blockA") || strings.Contains(path, "chrdevA"), "%s: mode != 0", path)
	require.True(s.t, a.Symlink == "", "%s: link != ''", path)
	line := s.opString("file", path, &a.FileAttrs)
	if s.attrs&fs.AttrsHash != 0 {
		da, err := src.DeriveAttrs()
		require.NoError(s.t, err)
		require.True(s.t, da.Hash != "" || da.HTTPInfo != "", "%s: hash|http == ''; hash=%q, http=%q", path, da.Hash, da.HTTPInfo)
		if da.Hash != "" {
			line += " hash=" + da.Hash
		}
		if da.URL != "" {
			line += " url=" + da.URL
		}
		if da.HTTPInfo != "" {
			line += " http=" + da.HTTPInfo
		}
	}
	s.Nodes = append(s.Nodes, line)
	s.Written = append(s.Written, line)
	s.WrittenPaths[filepath.Clean("/"+path)] = true
	return src, nil
}
func (s *WriterMock) Link(path, target string) error {
	s.link(path, target)
	line := path + " hlink=" + target
	s.Written = append(s.Written, line)
	s.WrittenPaths[filepath.Clean("/"+path)] = true
	return nil
}
func (s *WriterMock) LowerLink(path, target string, a *fs.NodeAttrs) error {
	s.link(path, target)
	return nil
}
func (s *WriterMock) link(path, target string) {
	require.True(s.t, target != "", "%s: link target must be provided", path)
	line := path + " hlink=" + target
	s.Nodes = append(s.Nodes, line)
}
func (s *WriterMock) Symlink(path string, a fs.FileAttrs) error {
	require.True(s.t, a.Symlink != "", "%s: symlink dest must be provided", path)
	line := s.opString(fs.TypeSymlink, path, &a)
	s.Nodes = append(s.Nodes, line)
	s.Written = append(s.Written, line)
	s.WrittenPaths[filepath.Clean("/"+path)] = true
	return nil
}
func (s *WriterMock) Dir(path, name string, a fs.FileAttrs) error {
	require.True(s.t, a.Mode != 0, "%s: mode != 0", path)
	require.True(s.t, a.Symlink == "", "%s: link != ''", path)
	line := s.opString(fs.TypeDir, path, &a)
	s.Nodes = append(s.Nodes, line)
	s.Written = append(s.Written, line)
	s.WrittenPaths[filepath.Clean("/"+path)] = true
	return nil
}
func (s *WriterMock) Fifo(path string, a fs.DeviceAttrs) error {
	line := s.opString(fs.TypeFifo, path, &a.FileAttrs)
	s.Nodes = append(s.Nodes, line)
	s.Written = append(s.Written, line)
	s.WrittenPaths[filepath.Clean("/"+path)] = true
	return nil
}
func (s *WriterMock) Device(path string, a fs.DeviceAttrs) error {
	line := s.opString(fs.TypeDevice, path, &a.FileAttrs)
	s.Nodes = append(s.Nodes, line)
	s.Written = append(s.Written, line)
	s.WrittenPaths[filepath.Clean("/"+path)] = true
	return nil
}
func (s *WriterMock) Remove(path string) error {
	line := path + " type=whiteout"
	s.Nodes = append(s.Nodes, line)
	s.Written = append(s.Written, line)
	s.WrittenPaths[filepath.Clean("/"+path)] = true
	return nil
}
func (s *WriterMock) opString(t fs.NodeType, path string, a *fs.FileAttrs) string {
	return encodePath(path) + " " + (&fs.NodeInfo{t, *a}).AttrString(s.attrs)
}

type ExpandingWriterMock struct {
	*WriterMock
}

func (s *ExpandingWriterMock) Lazy(path, name string, src fs.LazySource, written map[fs.Source]string) error {
	return src.Expand(path, s, written)
}

func encodePath(p string) string {
	l := strings.Split(p, string(filepath.Separator))
	for i, s := range l {
		l[i] = url.PathEscape(s)
	}
	return strings.Join(l, string(filepath.Separator))
}
