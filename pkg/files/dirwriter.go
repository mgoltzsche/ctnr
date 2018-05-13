package files

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/cyphar/filepath-securejoin"
	"github.com/mgoltzsche/cntnr/pkg/idutils"
	"github.com/mgoltzsche/cntnr/pkg/log"
	"github.com/openSUSE/umoci/oci/layer"
	"github.com/openSUSE/umoci/pkg/fseval"
	"github.com/openSUSE/umoci/pkg/system"
	"github.com/pkg/errors"
	"golang.org/x/sys/unix"
)

var _ Writer = &DirWriter{}

// A mapping file system writer that secures root directory boundaries.
// Derived from umoci's tar_extract.go to allow separate source/dest interfaces
// and filter archive contents on extraction
type DirWriter struct {
	dir        string
	idMappings idutils.IdMappings
	fsEval     fseval.FsEval
	rootless   bool
	warn       log.Logger
}

func NewDirWriter(dir string, opts FSOptions, warn log.Logger) (w *DirWriter) {
	return &DirWriter{
		dir:        dir,
		idMappings: opts.IdMappings,
		fsEval:     opts.FsEval,
		rootless:   opts.Rootless,
		warn:       warn,
	}
}

func (w *DirWriter) mkdirAll(dir string) (err error) {
	if err = w.fsEval.MkdirAll(dir, 0755); err != nil {
		err = errors.Wrap(err, "dir writer")
	}
	return
}

func (w *DirWriter) File(file string, src io.Reader, a FileAttrs) (err error) {
	if file, err = w.resolveParentDir(file); err != nil {
		return
	}
	if err = w.remove(file); err != nil {
		return
	}
	if err = w.mkdirAll(filepath.Dir(file)); err != nil {
		return
	}
	destFile, err := w.fsEval.Create(file)
	if err != nil {
		return
	}
	defer destFile.Close()
	n, err := io.Copy(destFile, src)
	if err != nil {
		return errors.Errorf("write to %s: %s", file, err)
	}
	if n != a.Size {
		err = errors.Wrap(io.ErrShortWrite, "copy file")
	}
	return w.writeMetadata(file, a)
}

func (w *DirWriter) Link(file string, a FileAttrs) (err error) {
	if !filepath.IsAbs(a.Link) {
		a.Link = filepath.Join(filepath.Dir(file), a.Link)
	}
	if file, err = w.resolveParentDir(file); err != nil {
		return
	}
	a.Link = layer.CleanPath(a.Link)
	linkDir, linkFile := filepath.Split(a.Link)
	if linkDir, err = securejoin.SecureJoinVFS(w.dir, linkDir, w.fsEval); err != nil {
		return errors.Wrap(err, "sanitise hardlink target in rootfs")
	}
	linkName := filepath.Join(linkDir, linkFile)
	if err = w.remove(file); err != nil {
		return
	}
	if err = w.mkdirAll(filepath.Dir(file)); err != nil {
		return
	}
	return w.fsEval.Link(linkName, file)
}

func (w *DirWriter) Symlink(file string, a FileAttrs) (err error) {
	if file, err = w.resolveParentDir(file); err != nil {
		return
	}
	if err = w.validateLink(file, a.Link); err != nil {
		return
	}
	if err = w.remove(file); err != nil {
		return
	}
	if err = w.mkdirAll(filepath.Dir(file)); err != nil {
		return
	}
	if err = w.fsEval.Symlink(a.Link, file); err != nil {
		return
	}
	return w.writeMetadata(file, a)
}

func (w *DirWriter) Fifo(file string, a FileAttrs) (err error) {
	if file, err = w.resolveParentDir(file); err != nil {
		return
	}
	if err = w.remove(file); err != nil {
		return
	}
	if err = w.mkdirAll(filepath.Dir(file)); err != nil {
		return
	}
	var dev system.Dev_t
	dev = system.Dev_t(unix.Mkdev(uint32(a.Devmajor), uint32(a.Devminor)))
	if err := w.fsEval.Mknod(file, a.Mode, dev); err != nil {
		return errors.Wrap(err, "mknod")
	}
	return
}

func (w *DirWriter) Block(file string, a FileAttrs) (err error) {
	if w.rootless {
		// Fake block device
		a0 := a
		a0.Mode = 0
		err = w.File(file, bytes.NewReader([]byte{}), a)
	} else {
		// Create as fifo
		err = w.Fifo(file, a)
	}
	return
}

func (w *DirWriter) Dir(dir string, a FileAttrs) (err error) {
	if dir, err = w.resolveParentDir(dir); err != nil {
		return
	}

	st, err := w.fsEval.Lstat(dir)
	exists := false
	if err == nil {
		if st.IsDir() {
			exists = true
		} else if err = w.fsEval.Remove(dir); err != nil {
			return
		}
	} else if !os.IsNotExist(errors.Cause(err)) {
		return errors.Wrap(err, "write dir")
	}
	if !exists {
		if err = w.mkdirAll(filepath.Dir(dir)); err != nil {
			return
		}
		if err = w.fsEval.Mkdir(dir, a.Mode); err != nil {
			return
		}
	}
	return w.writeMetadata(dir, a)
}

func (w *DirWriter) DirImplicit(dir string, a FileAttrs) (err error) {
	if dir, err = w.resolveFile(dir); err != nil {
		return
	}
	st, err := w.fsEval.Lstat(dir)
	if err == nil {
		if st.IsDir() {
			return
		} else {
			if err = w.fsEval.Remove(dir); err != nil {
				return
			}
		}
	} else if !os.IsNotExist(errors.Cause(err)) {
		return
	}
	if err = w.mkdirAll(dir); err != nil {
		return
	}
	return w.writeTimeMetadata(dir, a.Atime, a.Mtime)
}

func (w *DirWriter) Remove(file string) (err error) {
	if file, err = w.resolveParentDir(file); err != nil {
		return
	}
	return w.remove(file)
}

func (w *DirWriter) remove(realFile string) (err error) {
	if err = w.fsEval.RemoveAll(realFile); err != nil {
		err = errors.Wrap(err, "write dir")
	}
	return
}

func (w *DirWriter) writeMetadata(file string, a FileAttrs) (err error) {
	// chmod
	if a.Mode&os.ModeType != os.ModeSymlink {
		if err = w.fsEval.Chmod(file, a.Mode); err != nil {
			return errors.Wrap(err, "chmod")
		}
	}

	// chown
	if !w.rootless {
		// TODO: use fseval method if available
		var u idutils.UserIds
		if u, err = a.UserIds.ToHost(w.idMappings); err != nil {
			return errors.Wrapf(err, "write file metadata: %s", file)
		}
		if err = errors.Wrap(os.Lchown(file, int(u.Uid), int(u.Gid)), "chown"); err != nil {
			return
		}
	}

	// xattrs
	if err = w.fsEval.Lclearxattrs(file); err != nil {
		return errors.Wrapf(err, "clear xattrs: %s", file)
	}
	for _, a := range a.Xattrs {
		if err = w.fsEval.Lsetxattr(file, a.Key, a.Value, 0); err != nil {
			// In rootless mode, some xattrs will fail (security.capability).
			// This is _fine_ as long as not run as root
			if w.rootless && os.IsPermission(errors.Cause(err)) {
				w.warn.Printf("write file metadata: ignoring EPERM on setxattr %s: %v", a.Key, err)
				continue
			}
			return errors.Wrapf(err, "set xattr: %s", file)
		}
	}

	return w.writeTimeMetadata(file, a.Atime, a.Mtime)
}

func (w *DirWriter) writeTimeMetadata(file string, atime, mtime time.Time) error {
	if mtime.IsZero() {
		mtime = time.Now()
	}
	if atime.IsZero() {
		atime = mtime
	}
	err := w.fsEval.Lutimes(file, atime, mtime)
	return errors.Wrapf(err, "write file times: %s", file)
}

func (w *DirWriter) validateLink(file, target string) (err error) {
	if filepath.IsAbs(target) {
		target = filepath.Join(w.dir, target)
	} else {
		target = filepath.Join(filepath.Dir(file), target)
	}
	if !within(target, w.dir) {
		err = errors.Errorf("write dir: refused to write link %s with destination outside rootfs: %s", file, target)
	}
	return
}

func (w *DirWriter) resolveFile(path string) (r string, err error) {
	r, err = securejoin.SecureJoinVFS(w.dir, path, w.fsEval)
	err = errors.Wrap(err, "sanitise symlinks in rootfs")
	return
}

func (w *DirWriter) resolveParentDir(path string) (r string, err error) {
	dir, file := filepath.Split(path)
	r, err = w.resolveFile(dir)
	r = filepath.Join(r, file)
	return
}
