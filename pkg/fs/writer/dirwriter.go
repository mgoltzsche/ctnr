package writer

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/cyphar/filepath-securejoin"
	exterrors "github.com/mgoltzsche/cntnr/pkg/errors"
	"github.com/mgoltzsche/cntnr/pkg/fs"
	"github.com/mgoltzsche/cntnr/pkg/fs/source"
	"github.com/mgoltzsche/cntnr/pkg/log"
	"github.com/openSUSE/umoci/pkg/fseval"
	"github.com/pkg/errors"
	"golang.org/x/sys/unix"
)

var _ fs.Writer = &DirWriter{}

// A mapping file system writer that secures root directory boundaries.
// Derived from umoci's tar_extract.go to allow separate source/dest interfaces
// and filter archive contents on extraction
type DirWriter struct {
	dir        string
	dirTimes   map[string]fs.FileTimes
	attrMapper fs.AttrMapper
	fsEval     fseval.FsEval
	rootless   bool
	now        time.Time
	warn       log.Logger
}

func NewDirWriter(dir string, opts fs.FSOptions, warn log.Logger) (w *DirWriter) {
	var attrMapper fs.AttrMapper
	if opts.Rootless {
		attrMapper = fs.NewRootlessAttrMapper(opts.IdMappings)
	} else {
		attrMapper = fs.NewAttrMapper(opts.IdMappings)
	}
	return &DirWriter{
		dir:        dir,
		dirTimes:   map[string]fs.FileTimes{},
		attrMapper: attrMapper,
		fsEval:     opts.FsEval,
		rootless:   opts.Rootless,
		now:        time.Now(),
		warn:       warn,
	}
}

func (w *DirWriter) Close() (err error) {
	// Apply dir time metadata
	for dir, a := range w.dirTimes {
		if err = w.writeTimeMetadata(dir, a); err != nil {
			break
		}
	}
	return
}

func (w *DirWriter) Parent() error {
	return nil
}

func (w *DirWriter) LowerNode(path, name string, a *fs.NodeAttrs) (err error) {
	return errors.Errorf("dirwriter: operation not supported: write node (%s) from parsed fs spec. hint: load FsSpec from dir", path)
}

func (w *DirWriter) LowerLink(path, target string, a *fs.NodeAttrs) error {
	return errors.Errorf("dirwriter: operation not supported: write link (%s) from parsed fs spec. hint: load FsSpec from dir", path)
}

func (w *DirWriter) Lazy(path, name string, src fs.LazySource, written map[fs.Source]string) (err error) {
	return src.Expand(path, w, written)
}

func (w *DirWriter) mkdirAll(dir string) (err error) {
	if err = w.fsEval.MkdirAll(dir, 0755); err != nil {
		err = errors.Wrap(err, "dir writer")
	}
	return
}

func (w *DirWriter) File(file string, src fs.FileSource) (r fs.Source, err error) {
	if file, err = w.resolveParentDir(file); err != nil {
		return
	}
	a := src.Attrs()
	f, err := src.Reader()
	if err != nil {
		return
	}
	defer func() {
		err = exterrors.Append(err, errors.WithMessage(f.Close(), "write file"))
	}()
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
	n, err := io.Copy(destFile, f)
	if err != nil {
		return nil, errors.Errorf("write to %s: %s", file, err)
	}
	if n != a.Size {
		return nil, errors.Wrap(io.ErrShortWrite, "copy file")
	}
	a.Size = n
	err = w.writeMetadataChmod(file, a.FileAttrs)
	return source.NewSourceFileHashed(fs.NewFileReader(file, fseval.RootlessFsEval), a.FileAttrs, src.HashIfAvailable()), errors.Wrap(err, "copy file")
}

func (w *DirWriter) Link(file, target string) (err error) {
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(file), target)
	}
	if file, err = w.resolveParentDir(file); err != nil {
		return
	}
	linkName, err := w.resolveParentDir(target)
	if err != nil {
		return
	}
	if err = w.remove(file); err != nil {
		return
	}
	if err = w.mkdirAll(filepath.Dir(file)); err != nil {
		return
	}
	return w.fsEval.Link(linkName, file)
}

func (w *DirWriter) Symlink(path string, a fs.FileAttrs) (err error) {
	file, err := w.resolveParentDir(path)
	if err != nil {
		return
	}
	a.Symlink, err = normalizeLinkDest(path, a.Symlink)
	if err != nil {
		return
	}
	if err = w.remove(file); err != nil {
		return
	}
	if err = w.mkdirAll(filepath.Dir(file)); err != nil {
		return
	}
	if err = w.fsEval.Symlink(a.Symlink, file); err != nil {
		return
	}
	if err = w.writeMetadata(file, a); err != nil {
		return
	}
	return w.writeTimeMetadata(file, a.FileTimes)
}

func normalizeLinkDest(path, target string) (r string, err error) {
	target = filepath.Clean(target)
	r = target
	basePath := filepath.Dir(string(os.PathSeparator) + path)
	basePath, _ = filepath.Rel(string(os.PathSeparator), basePath)
	abs := filepath.IsAbs(r)
	if !abs {
		r = filepath.Join(basePath, r)
	}
	if abs || !isValidPath(r) {
		r, err = normalize(r)
		if err == nil {
			r = filepath.Clean(string(os.PathSeparator) + r)
			if !abs {
				r, err = filepath.Rel(string(os.PathSeparator)+basePath, r)
			}
		}
		return r, errors.Wrapf(err, "normalize link %s dest", path)
	}
	return target, nil
}

func (w *DirWriter) Fifo(file string, a fs.DeviceAttrs) (err error) {
	a.Mode = syscall.S_IFIFO | a.Mode.Perm()
	return w.device(file, a)
}

func (w *DirWriter) device(file string, a fs.DeviceAttrs) (err error) {
	if file, err = w.resolveParentDir(file); err != nil {
		return
	}
	if err = w.remove(file); err != nil {
		return
	}
	if err = w.mkdirAll(filepath.Dir(file)); err != nil {
		return
	}
	dev := unix.Mkdev(uint32(a.Devmajor), uint32(a.Devminor))
	if err := w.fsEval.Mknod(file, a.Mode, dev); err != nil {
		return errors.Wrap(err, "mknod")
	}
	return w.writeMetadataChmod(file, a.FileAttrs)
}

func (w *DirWriter) Device(path string, a fs.DeviceAttrs) (err error) {
	if w.rootless {
		w.warn.Println("dirwriter: faking device in rootless mode: " + path)
		_, err = w.File(path, source.NewSourceFile(fs.NewReadableBytes([]byte{}), a.FileAttrs))
	} else {
		err = w.device(path, a)
	}
	return
}

func (w *DirWriter) Mkdir(dir string) (err error) {
	return w.mkdirAll(dir)
}

func (w *DirWriter) Dir(dir, base string, a fs.FileAttrs) (err error) {
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
	// write metadata
	if err = w.fsEval.Chmod(dir, a.Mode); err != nil {
		return errors.Wrap(err, "chmod")
	}
	if err = w.writeMetadata(dir, a); err != nil {
		return
	}
	w.dirTimes[dir] = a.FileTimes
	return
}

func (w *DirWriter) Remove(file string) (err error) {
	if file, err = w.resolveParentDir(file); err != nil {
		return
	}
	return w.remove(file)
}

func (w *DirWriter) remove(realFile string) (err error) {
	if err = w.fsEval.RemoveAll(realFile); err != nil {
		return errors.Wrap(err, "write dir")
	}
	delete(w.dirTimes, realFile)
	return
}

func (w *DirWriter) writeMetadataChmod(file string, a fs.FileAttrs) (err error) {
	// chmod
	if err = w.fsEval.Chmod(file, a.Mode); err != nil {
		return errors.Wrap(err, "chmod")
	}
	if err = w.writeMetadata(file, a); err != nil {
		return
	}
	return w.writeTimeMetadata(file, a.FileTimes)
}

func (w *DirWriter) writeMetadata(file string, a fs.FileAttrs) (err error) {
	// chown
	if err = w.attrMapper.ToHost(&a); err != nil {
		return errors.Wrapf(err, "write file metadata: %s", file)
	}
	if !w.rootless {
		// TODO: use fseval method if available
		if err = errors.Wrap(os.Lchown(file, int(a.Uid), int(a.Gid)), "chown"); err != nil {
			return
		}
	}

	// xattrs
	if err = w.fsEval.Lclearxattrs(file); err != nil {
		return errors.Wrapf(err, "clear xattrs: %s", file)
	}
	for k, v := range a.Xattrs {
		if e := w.fsEval.Lsetxattr(file, k, []byte(v), 0); e != nil {
			// In rootless mode, some xattrs will fail (security.capability).
			// This is _fine_ as long as not run as root
			if w.rootless && os.IsPermission(errors.Cause(e)) {
				w.warn.Printf("write file metadata: %s: ignoring EPERM on setxattr %s: %v", file[len(w.dir):], k, e)
				continue
			}
			return errors.Wrapf(e, "set xattr: %s", file)
		}
	}
	return
}

func (w *DirWriter) writeTimeMetadata(file string, t fs.FileTimes) error {
	if t.Mtime.IsZero() {
		t.Mtime = w.now.UTC()
	}
	if t.Atime.IsZero() {
		t.Atime = t.Mtime
	}
	if err := w.fsEval.Lutimes(file, t.Atime, t.Mtime); !os.IsNotExist(errors.Cause(err)) {
		return errors.Wrapf(err, "write file times: %s", file)
	}
	return nil
}

func (w *DirWriter) validateLink(path, file, target string) (err error) {
	dest := target
	if filepath.IsAbs(dest) {
		dest = filepath.Join(w.dir, dest)
	} else {
		dest = filepath.Join(filepath.Dir(file), dest)
	}
	if !within(dest, w.dir) {
		err = errors.Errorf("refused to write link %s with destination outside rootfs: %s", path, target)
	}
	return
}

// true if file is within rootDir
func within(file, rootDir string) bool {
	a := string(filepath.Separator)
	return strings.Index(file+a, rootDir+a) == 0
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
