package fs

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mgoltzsche/cntnr/pkg/idutils"
	"github.com/pkg/errors"
)

const (
	AttrsHash    AttrSet = 1
	AttrsMtime   AttrSet = 2
	AttrsAtime   AttrSet = 4
	AttrsAll             = AttrsHash | AttrsMtime | AttrsAtime
	AttrsCompare         = AttrsHash | AttrsMtime
	TimeFormat           = time.RFC3339
)

type AttrSet uint

var (
	TypeFile     NodeType = "file"
	TypeDir      NodeType = "dir"
	TypeOverlay  NodeType = "overlay"
	TypeSymlink  NodeType = "symlink"
	TypeFifo     NodeType = "fifo"
	TypeDevice   NodeType = "dev"
	TypeWhiteout NodeType = "whiteout"
	nameRegex             = regexp.MustCompile("^[a-zA-Z0-9\\-_\\.,]+$")
	nilWriter             = hashingNilWriter("noop writer")
	_            Source   = &NodeAttrs{}
)

type NodeType string

type FileAttrs struct {
	Mode os.FileMode
	idutils.UserIds
	Xattrs map[string]string
	FileTimes
	Size    int64
	Symlink string
}

func (a *FileAttrs) AttrString(attrs AttrSet) string {
	s := ""
	if !a.UserIds.IsZero() {
		s = "usr=" + a.UserIds.String()
	}
	perm := a.Mode.Perm()
	if perm != 0 && a.Mode&os.ModeSymlink == 0 {
		s += " mode=" + strconv.FormatUint(uint64(perm), 8)
	}
	if a.Size > 0 {
		s += " size=" + strconv.Itoa(int(a.Size))
	}
	if a.Symlink != "" {
		s += " link=" + encodePath(a.Symlink)
	}
	if len(a.Xattrs) > 0 {
		xa := make([]string, 0, len(a.Xattrs))
		for k, v := range a.Xattrs {
			xa = append(xa, fmt.Sprintf("xattr.%s=%s", url.PathEscape(k), url.PathEscape(v)))
		}
		sort.Strings(xa)
		s += " " + strings.Join(xa, " ")
	}
	if attrs&AttrsMtime != 0 {
		if !a.Mtime.IsZero() {
			s += " mtime=" + fmt.Sprintf("%d", a.Mtime.Unix())
		}
	}
	if attrs&AttrsAtime != 0 {
		if !a.Atime.IsZero() {
			s += " atime=" + fmt.Sprintf("%d", a.Atime.Unix())
		}
	}
	return strings.TrimLeft(s, " ")
}

func (a *FileAttrs) Equal(o *FileAttrs) bool {
	return a.Mode.Perm() == o.Mode.Perm() && a.Uid == o.Uid && a.Gid == o.Gid &&
		a.Mtime.Unix() == o.Mtime.Unix() && a.Size == o.Size && a.Symlink == o.Symlink &&
		xattrsEqual(a.Xattrs, o.Xattrs)
}

func xattrsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	if a != nil {
		for k, v := range a {
			if bv, ok := b[k]; !ok || v != bv {
				return false
			}
		}
	}
	return true
}

type FileTimes struct {
	Atime time.Time
	Mtime time.Time
}

type NodeAttrs struct {
	NodeInfo
	DerivedAttrs
}

type DerivedAttrs struct {
	Hash     string
	URL      string
	HTTPInfo string
}

func (s *DerivedAttrs) Equal(o *DerivedAttrs) bool {
	return s.Hash == o.Hash && s.URL == o.URL && s.HTTPInfo == o.HTTPInfo
}

type NodeInfo struct {
	NodeType NodeType
	FileAttrs
}

func (s NodeInfo) Equal(o NodeInfo) bool {
	return s.NodeType == o.NodeType && s.FileAttrs.Equal(&o.FileAttrs)
}

func (a *NodeInfo) AttrString(attrs AttrSet) string {
	return "type=" + string(a.NodeType) + " " + a.FileAttrs.AttrString(attrs)
}

type DeviceAttrs struct {
	FileAttrs
	Devmajor int64
	Devminor int64
}

func (s *NodeAttrs) Attrs() NodeInfo {
	return s.NodeInfo
}
func (s *NodeAttrs) DeriveAttrs() (DerivedAttrs, error) {
	return s.DerivedAttrs, nil
}
func (s *NodeAttrs) Write(path, name string, w Writer, written map[Source]string) (err error) {
	if linkDest, ok := written[s]; ok {
		err = w.LowerLink(path, linkDest, s)
	} else {
		written[s] = path
		err = w.LowerNode(path, name, s)
	}
	return
}

func ParseNodeAttrs(s string) (r NodeAttrs, err error) {
	for _, e := range strings.Split(s, " ") {
		l := strings.Split(e, "=")
		if len(l) != 2 {
			return r, errors.Errorf("parse file attrs: invalid attr %q in %q (missing '=')", e, s)
		}

		k := l[0]
		v := l[1]
		switch k {
		case "type":
			r.NodeType = NodeType(v)
		case "mode":
			var m uint64
			m, err = strconv.ParseUint(v, 8, 32)
			if err != nil {
				return r, errors.Errorf("parse file attrs: invalid mode %q", v)
			}
			r.Mode = os.FileMode(m)
		case "usr":
			r.UserIds, err = idutils.ParseUser(v).ToIds()
			if err != nil {
				return r, errors.Wrap(err, "parse file attrs")
			}
		case "size":
			r.Size, err = strconv.ParseInt(v, 10, 64)
			if err != nil {
				return r, errors.Errorf("parse file attrs: invalid size %q", v)
			}
		case "link":
			r.Symlink, err = decodePath(v)
			if err != nil {
				return r, errors.Errorf("parse file attrs: invalid symlink %q", v)
			}
		case "hash":
			r.Hash = v
		case "url":
			r.URL = v
		case "http":
			if r.HTTPInfo, err = url.QueryUnescape(v); err != nil {
				return r, errors.Wrap(err, "parse file attrs: invalid http attr")
			}
		case "mtime":
			tme, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				return r, errors.Wrap(err, "parse file attrs: invalid mtime")
			}
			r.Mtime = time.Unix(tme, 0)
		case "atime":
			tme, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				return r, errors.Wrap(err, "parse file attrs: invalid atime")
			}
			r.Atime = time.Unix(tme, 0)
		default:
			if strings.HasPrefix(k, "xattr.") {
				k, err = url.PathUnescape(k[6:])
				if err != nil {
					return r, errors.Errorf("parse file attrs: invalid xattr key %q", k)
				}
				v, err = url.PathUnescape(v)
				if err != nil {
					return r, errors.Errorf("parse file attrs: invalid xattr value %q", v)
				}
				if r.Xattrs == nil {
					r.Xattrs = map[string]string{}
				}
				r.Xattrs[k] = v
			}
		}
	}
	if r.Mtime.IsZero() {
		r.Mtime = time.Now()
	}
	if r.Atime.IsZero() {
		r.Atime = r.Mtime
	}
	return
}

func parseUnixTime(s string) (t time.Time, err error) {
	dotPos := strings.Index(s, ".")
	if dotPos == -1 && dotPos+1 < len(s) {
		return t, errors.Errorf("parse unix time %q", s)
	}
	sec, err := strconv.ParseInt(s[:dotPos], 10, 64)
	if err != nil {
		return t, errors.Errorf("parse unix second from %q", s)
	}
	nsec, err := strconv.ParseInt(s[dotPos+1:], 10, 64)
	if err != nil {
		return t, errors.Errorf("parse nanosecond from %q", s)
	}
	return time.Unix(sec, nsec), nil
}

func (a *NodeAttrs) AttrString(attrs AttrSet) string {
	s := a.NodeInfo.AttrString(attrs)
	if attrs&AttrsHash != 0 {
		if a.Hash != "" {
			s += " hash=" + a.Hash
		}
		if a.URL != "" {
			s += " url=" + a.URL
			if a.HTTPInfo != "" {
				s += " http=" + url.QueryEscape(a.HTTPInfo)
			}
		}
	}
	return s
}

func (a *NodeAttrs) String() string {
	return "NodeAttrs{" + a.AttrString(AttrsAll) + "}"
}

func encodePath(p string) string {
	l := strings.Split(p, string(filepath.Separator))
	for i, s := range l {
		l[i] = url.PathEscape(s)
	}
	return strings.Join(l, string(filepath.Separator))
}

func decodePath(p string) (r string, err error) {
	l := strings.Split(p, string(filepath.Separator))
	for i, s := range l {
		if l[i], err = url.PathUnescape(s); err != nil {
			return "", errors.Wrap(err, "decode path")
		}
	}
	return strings.Join(l, string(filepath.Separator)), nil
}

type FileInfo struct {
	name string
	*FileAttrs
}

func NewFileInfo(name string, attrs *FileAttrs) *FileInfo {
	return &FileInfo{name, attrs}
}

var _ os.FileInfo = &FileInfo{}

func (fi *FileInfo) IsDir() bool {
	return fi.Mode()&os.ModeDir != 0
}
func (fi *FileInfo) ModTime() time.Time {
	return fi.Mtime
}
func (fi *FileInfo) Mode() os.FileMode {
	return fi.FileAttrs.Mode
}
func (fi *FileInfo) Name() string {
	return fi.name
}
func (fi *FileInfo) Size() int64 {
	return fi.FileAttrs.Size
}
func (fi *FileInfo) Sys() interface{} {
	return fi
}
