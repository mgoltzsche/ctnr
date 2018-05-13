package files

import (
	"archive/tar"
	"compress/bzip2"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"syscall"
	"time"

	"github.com/mgoltzsche/cntnr/pkg/idutils"
	"github.com/openSUSE/umoci/pkg/fseval"
	"github.com/pkg/errors"
)

type SourceCollector func(src Source, path string) error

func WalkSources(source string, collector SourceCollector) error {
	return nil
}

type Readable interface {
	Reader() (io.ReadCloser, error)
}

type Sources struct {
	fsEval    fseval.FsEval
	idMap     idutils.IdMappings
	sourceMap map[string]Source
}

func NewSources(fsEval fseval.FsEval, idMap idutils.IdMappings) *Sources {
	return &Sources{fsEval, idMap, map[string]Source{}}
}

func (s *Sources) File(file string, fi os.FileInfo, usr *idutils.UserIds) (r Source, err error) {
	a, err := s.fileAttrs(file, fi, usr)
	if err != nil {
		return
	}
	fm := fi.Mode()
	switch {
	case fm.IsRegular():
		r = NewSourceFile(file, a)
	case fm.IsDir():
		r = NewSourceDir(a)
	case fm&os.ModeSymlink != 0:
		r = NewSourceSymlink(a)
	case fm&os.ModeDevice != 0:
		r = NewSourceBlock(a)
	case fm&os.ModeNamedPipe != 0:
		r = NewSourceFifo(a)
	case fm&os.ModeSocket != 0:
		return nil, errors.Errorf("source: sockets not supported (%s)", file)
	default:
		return nil, errors.Errorf("source: unknown file mode %v in %s", fm, file)
	}

	st := fi.Sys().(*syscall.Stat_t)
	if st.Nlink > 1 {
		// Handle hardlink - more than one path point to this node
		inodeKey := fmt.Sprintf("%x:%x", st.Dev, st.Ino)
		src := s.sourceMap[inodeKey]
		if src == nil {
			r = NewSourceLink(r)
			s.sourceMap[inodeKey] = r
		} else {
			a := src.Attrs()
			if usr == nil || usr.Uid == a.Uid && usr.Gid == a.Gid {
				r = src
			}
		}
	}

	// TODO: detect hardlink by inode - don't do it here but link within fs node
	// by wrapping existing node's source in impl that delegates Attrs() as is but writes actual file only once, otherwise link
	// -> how does it know about where to link -> fixed relativized link path set when wrapped
	// => Better: resolve link during write by mapping previously written sources to paths
	// -> what if one of both hardlinks gets replaced with a different file?
	//     -> bidirectionally link the nodes
	//     -> hard link is not connected anymore but other links still work -> fine
	return
}

func (s *Sources) FileOverlay(file string, fi os.FileInfo, usr *idutils.UserIds) (r Source, err error) {
	if fi.Mode().IsRegular() {
		return s.sourceMaybeOverlay(file, fi, usr)
	}
	return s.File(file, fi, usr)
}

func (s *Sources) fileAttrs(file string, si os.FileInfo, usr *idutils.UserIds) (r FileAttrs, err error) {
	// uid/gid
	if usr == nil {
		st := si.Sys().(*syscall.Stat_t)
		u := idutils.UserIds{uint(st.Uid), uint(st.Gid)}
		u, err = u.ToContainer(s.idMap)
		usr = &u
	}
	// permissions
	r.Mode = si.Mode()
	// size
	r.Size = si.Size()
	// atime/mtime
	r.Atime, r.Mtime = fileTime(si)
	if r.Mtime.IsZero() {
		r.Mtime = time.Now()
	}
	if r.Atime.IsZero() {
		r.Atime = r.Mtime
	}
	// xattrs
	xattrs, err := s.fsEval.Llistxattr(file)
	if err != nil {
		return r, errors.Wrap(err, "list xattrs of "+file)
	}
	for _, name := range xattrs {
		value, e := s.fsEval.Lgetxattr(file, name)
		if e != nil {
			return r, errors.Wrapf(e, "get xattr %s of %s", name, file)
		}
		r.Xattrs = append(r.Xattrs, XAttr{name, value})
	}
	// link
	if r.Mode&os.ModeSymlink != 0 {
		r.Link, err = s.fsEval.Readlink(file)
	}
	return
}

func fileTime(st os.FileInfo) (atime, mtime time.Time) {
	stu := st.Sys().(*syscall.Stat_t)
	atime = time.Unix(int64(stu.Atim.Sec), int64(stu.Atim.Nsec))
	mtime = st.ModTime()
	return
}

// Creates source for archive or simple file as fallback
func (s *Sources) sourceMaybeOverlay(file string, fi os.FileInfo, usr *idutils.UserIds) (src Source, err error) {
	// Try open tar file
	f, err := os.Open(file)
	if err != nil {
		return nil, errors.Wrap(err, "overlay source")
	}
	defer func() {
		if err != nil {
			err = errors.Wrapf(err, "overlay source %s", file)
		}
	}()
	defer f.Close()
	var r io.Reader = f
	isGzip := false
	isBzip2 := false

	// Try to decompress gzip
	gr, err := gzip.NewReader(r)
	if err == nil {
		r = gr
		isGzip = true
	} else if err != io.EOF && err != io.ErrUnexpectedEOF && err != gzip.ErrHeader {
		return
	} else if _, err = f.Seek(0, 0); err != nil {
		return
	}

	// Try to decompress bzip2
	if !isGzip && err != io.EOF {
		br := bzip2.NewReader(r)
		b := make([]byte, 512)
		_, err = br.Read(b)
		if err == nil {
			r = br
			isBzip2 = true
		} else if err != io.EOF && err != io.ErrUnexpectedEOF {
			if _, ok := err.(bzip2.StructuralError); !ok {
				return
			}
		}
		if _, err = f.Seek(0, 0); err != nil {
			return
		}
	}

	// Try to read as tar
	tr := tar.NewReader(r)
	if _, err = tr.Next(); err == nil {
		attrs, err := s.fileAttrs(file, fi, usr)
		if err != nil {
			return nil, err
		}
		if isGzip {
			src = NewSourceTarGz(file, attrs)
		} else if isBzip2 {
			src = NewSourceTarBz(file, attrs)
		} else {
			src = NewSourceTar(file, attrs)
		}
		return src, nil
	} else if err != io.EOF && err != io.ErrUnexpectedEOF && err != tar.ErrHeader {
		return
	}

	// Tread as ordinary file if no compressed archive detected
	return s.File(file, fi, usr)
}
