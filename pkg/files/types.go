package files

import (
	"io"
	"os"
	"time"

	"github.com/mgoltzsche/cntnr/pkg/idutils"
)

// TODO: build mtree from the provided file pattern/URLs and
//       use it to generate a cacheable change value before downloading/copying input if necessary.
//       Source referenced as xattr? -> maybe use custom tree with each entry holding: Source, destination, metadata.
//       How to handle archive overlays in mtree? -> build custom impl

var (
	TypeFile    SourceType = "file"
	TypeDir     SourceType = "dir"
	TypeOverlay SourceType = "overlay"
	TypeSymlink SourceType = "symlink"
	TypeLink    SourceType = "link"
)

type SourceType string

func (t SourceType) String() string {
	return string(t)
}

func (t SourceType) IsFile() bool {
	return t == TypeFile
}

func (t SourceType) IsDir() bool {
	return t == TypeDir
}

func (t SourceType) IsSymlink() bool {
	return t == TypeSymlink
}

func (t SourceType) IsOverlay() bool {
	return t == TypeOverlay
}

type Source interface {
	Type() SourceType
	Attrs() *FileAttrs
	Hash() (string, error)
	WriteFiles(dest string, w Writer) error
}

type Writer interface {
	File(path string, src io.Reader, attrs FileAttrs) error
	Link(path string, attrs FileAttrs) error
	Symlink(path string, attrs FileAttrs) error
	Dir(path string, attrs FileAttrs) error
	DirImplicit(path string, attrs FileAttrs) error
	Fifo(path string, attrs FileAttrs) error
	Block(path string, attrs FileAttrs) error
	Remove(path string) error
}

type FileAttrs struct {
	Mode os.FileMode
	idutils.UserIds
	Xattrs   []XAttr
	Link     string
	Size     int64
	Atime    time.Time // not hashed
	Mtime    time.Time // not hashed
	Devmajor int64     // not hashed
	Devminor int64     // not hashed
}

// TODO: sort xattrs
type XAttr struct {
	Key   string
	Value []byte
}
