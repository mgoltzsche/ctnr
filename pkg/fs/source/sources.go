package source

import (
	"archive/tar"
	"compress/bzip2"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"syscall"
	"time"

	"github.com/mgoltzsche/cntnr/pkg/fs"
	"github.com/mgoltzsche/cntnr/pkg/idutils"
	"github.com/openSUSE/umoci/pkg/fseval"
	"github.com/openSUSE/umoci/pkg/system"
	"github.com/pkg/errors"
)

type Sources struct {
	fsEval     fseval.FsEval
	attrMapper fs.AttrMapper
	sourceMap  map[string]fs.Source
}

func NewSources(fsEval fseval.FsEval, attrMapper fs.AttrMapper) *Sources {
	return &Sources{fsEval, attrMapper, map[string]fs.Source{}}
}

func (s *Sources) File(file string, fi os.FileInfo, usr *idutils.UserIds) (r fs.Source, err error) {
	a, err := s.fileAttrs(file, fi, usr)
	if err != nil {
		return
	}
	fm := fi.Mode()
	var da fs.DeviceAttrs
	switch {
	case fm.IsRegular():
		a.Size = fi.Size()
		r = NewSourceFile(fs.NewFileReader(file, s.fsEval), a)
	case fm.IsDir():
		r = NewSourceDir(a)
	case fm&os.ModeSymlink != 0:
		a.Symlink, err = s.fsEval.Readlink(file)
		r = NewSourceSymlink(a)
	case fm&os.ModeDevice != 0 || fm&os.ModeCharDevice != 0:
		da, err = s.devAttrs(file, a)
		r = NewSourceDevice(da)
	case fm&os.ModeNamedPipe != 0:
		da, err = s.devAttrs(file, a)
		r = NewSourceFifo(da)
	case fm&os.ModeSocket != 0:
		return nil, errors.Errorf("source: sockets not supported (%s)", file)
	default:
		return nil, errors.Errorf("source: unknown file mode %v in %s", fm, file)
	}
	if err != nil {
		return nil, errors.Wrap(err, "source file")
	}

	st := fi.Sys().(*syscall.Stat_t)
	if st.Nlink > 1 {
		// Handle hardlink - more than one path point to this node.
		// Make sure a separate file is used if the user is different since same
		// user is always applied on all hardlinks
		inode := fmt.Sprintf("%x:%x:%d:%d", st.Dev, st.Ino, a.Uid, a.Gid)
		src := s.sourceMap[inode]
		if src == nil {
			//r = NewSourceLink(inode, UpperLink, r)
			s.sourceMap[inode] = r
		} else {
			r = src
		}
	}
	return
}

func (s *Sources) devAttrs(file string, a fs.FileAttrs) (r fs.DeviceAttrs, err error) {
	st, err := s.fsEval.Lstatx(file)
	if err != nil {
		return
	}
	r.FileAttrs = a
	r.Devmajor = int64(system.Majordev(system.Dev_t(st.Rdev)))
	r.Devminor = int64(system.Minordev(system.Dev_t(st.Rdev)))
	return
}

func (s *Sources) FileOverlay(file string, fi os.FileInfo, usr *idutils.UserIds) (r fs.Source, err error) {
	if fi.Mode().IsRegular() {
		return s.sourceMaybeOverlay(file, fi, usr)
	}
	return s.File(file, fi, usr)
}

func (s *Sources) fileAttrs(file string, si os.FileInfo, usr *idutils.UserIds) (r fs.FileAttrs, err error) {
	/*symlink := ""
	if si.Mode()&os.ModeSymlink != 0 {
		if symlink, err = s.fsEval.Readlink(file); err != nil {
			return r, errors.Wrap(err, "file attrs")
		}
	}
	hdr, err := tar.FileInfoHeader(si, symlink)
	if err != nil {
		return r, errors.Wrap(err, "file attrs")
	}*/

	// permissions
	r.Mode = si.Mode()
	// atime/mtime
	r.Atime, r.Mtime = fileTime(si)
	// xattrs
	xattrs, err := s.fsEval.Llistxattr(file)
	if err != nil {
		return r, errors.Wrap(err, "list xattrs of "+file)
	}
	if len(xattrs) > 0 {
		r.Xattrs = map[string]string{}
		for _, name := range xattrs {
			value, e := s.fsEval.Lgetxattr(file, name)
			if e != nil {
				return r, errors.Wrapf(e, "get xattr %s of %s", name, file)
			}
			r.Xattrs[name] = string(value)
		}
	}
	// uid/gid
	if usr == nil {
		st := si.Sys().(*syscall.Stat_t)
		r.UserIds = idutils.UserIds{uint(st.Uid), uint(st.Gid)}
		if err = s.attrMapper.ToContainer(&r); err != nil {
			return r, errors.Wrapf(err, "source file %s", file)
		}
	}
	// TODO: make sure user.rootlesscontainers xattr is removed
	if usr != nil {
		r.UserIds = *usr
	}
	return
}

func fileTime(st os.FileInfo) (atime, mtime time.Time) {
	stu := st.Sys().(*syscall.Stat_t)
	atime = time.Unix(int64(stu.Atim.Sec), int64(stu.Atim.Nsec)).UTC()
	mtime = st.ModTime().UTC()
	return
}

// Creates source for archive or simple file as fallback
func (s *Sources) sourceMaybeOverlay(file string, fi os.FileInfo, usr *idutils.UserIds) (src fs.Source, err error) {
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
		if isGzip {
			src = NewSourceTarGz(file)
		} else if isBzip2 {
			src = NewSourceTarBz(file)
		} else {
			src = NewSourceTar(file)
		}
		return src, nil
	} else if err != io.EOF && err != io.ErrUnexpectedEOF && err != tar.ErrHeader {
		return
	}

	// Treat as ordinary file if no compressed archive detected
	return s.File(file, fi, usr)
}
