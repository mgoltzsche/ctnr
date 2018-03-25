package files

import (
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/openSUSE/umoci/pkg/fseval"
	"github.com/pkg/errors"
)

type Logger interface {
	Printf(fmt string, o ...interface{})
}

type FileSystemBuilder struct {
	root        string
	dirs        map[string]bool
	latestMtime time.Time
	fsEval      fseval.FsEval
	rootless    bool
	log         Logger
}

func NewFileSystemBuilder(root string, rootless bool, logger Logger) *FileSystemBuilder {
	fsEval := fseval.DefaultFsEval
	if rootless {
		fsEval = fseval.RootlessFsEval
	}
	return &FileSystemBuilder{
		root:     filepath.Clean(root),
		dirs:     map[string]bool{},
		fsEval:   fsEval,
		rootless: rootless,
		log:      logger,
	}
}

func (s *FileSystemBuilder) Add(src []string, dest string) (r []string, err error) {
	sort.Strings(src)
	destc := filepath.Clean(dest)
	if strings.Index(destc, "../") == 0 || strings.Index(destc, "/../") == 0 || destc == ".." || destc == "/.." {
		return nil, errors.Errorf("destination %q is outside root directory", dest)
	}

	for _, file := range src {
		destDir := dest
		destFile := filepath.Base(file)
		if len(src) == 1 && len(dest) > 0 && destDir[len(dest)-1] != '/' {
			// Use dest as file name without appending src file name
			// if there is only one source file and dest does not end with '/'
			destDir = filepath.Dir(dest)
			destFile = filepath.Base(dest)
		}
		if err = s.add(file, destDir, destFile); err != nil {
			break
		}
		destFile = filepath.Join(s.root, destDir, destFile)
		destFile, err = filepath.Rel(s.root, destFile)
		if err != nil {
			break
		}
		r = append(r, "/"+destFile)
	}
	if err == nil {
		err = s.restoreTimeMetadata(s.root, s.latestMtime, s.latestMtime)
	}
	err = errors.Wrap(err, "add")
	return
}

func (s *FileSystemBuilder) add(src, destDir, destFile string) (err error) {
	src = filepath.Clean(src)
	destDir = filepath.Join(s.root, destDir)
	destFile = filepath.Join(destDir, destFile)
	destDir = filepath.Dir(destFile)
	st, err := s.fsEval.Lstat(src)
	if err != nil {
		return errors.Wrap(err, "add")
	}
	if !s.dirs[destDir] && within(destDir, s.root) {
		stu := st.Sys().(*syscall.Stat_t)
		atime := time.Unix(int64(stu.Atim.Sec), int64(stu.Atim.Nsec))
		mtime := st.ModTime()
		if err = s.mkdirAll(destDir, 0755, atime, mtime); err != nil {
			return errors.Wrap(err, "add")
		}
		s.dirs[destDir] = true
	}
	return s.copy(src, st, destFile)
}

// Recursively creates directories if they do not yet exist applying the provided mode, atime, mtime
func (s *FileSystemBuilder) mkdirAll(path string, mode os.FileMode, atime, mtime time.Time) (err error) {
	st, err := os.Stat(path)
	if err == nil {
		if st.IsDir() {
			return
		} else {
			if err = s.fsEval.Remove(path); err != nil {
				return
			}
		}
	} else if !os.IsNotExist(err) {
		return
	}
	if err = s.mkdirAll(filepath.Dir(path), mode, atime, mtime); err != nil {
		return
	}
	if err = s.fsEval.Mkdir(path, mode); err != nil {
		return
	}
	return s.restoreTimeMetadata(path, atime, mtime)
}

func (s *FileSystemBuilder) copy(src string, si os.FileInfo, dest string) (err error) {
	switch {
	case si.IsDir():
		return s.copyDir(src, dest)
	case si.Mode()&os.ModeSymlink != 0:
		return s.copyLink(src, si, dest)
	default:
		return s.copyFile(src, si, dest)
	}
	return
}

func (s *FileSystemBuilder) copyDir(src, dest string) (err error) {
	if err = s.createAllDirs(src, dest); err != nil {
		return
	}
	files, err := ioutil.ReadDir(src)
	if err != nil {
		return
	}
	for _, f := range files {
		if err = s.copy(filepath.Join(src, f.Name()), f, filepath.Join(dest, f.Name())); err != nil {
			return
		}
	}
	return
}

func (s *FileSystemBuilder) copyLink(src string, si os.FileInfo, dest string) (err error) {
	target, err := s.fsEval.Readlink(src)
	if err != nil {
		return
	}
	target = filepath.Clean(target)
	if !filepath.IsAbs(target) && !within(filepath.Join(filepath.Dir(dest), target), s.root) {
		return errors.Errorf("link %s target %q outside root directory", dest, target)
	}
	s.logCopy(src, dest, 0) // TODO: set correct link file mode
	if err = s.createAllDirs(filepath.Dir(src), filepath.Dir(dest)); err != nil {
		return
	}
	if e := s.fsEval.RemoveAll(dest); e != nil {
		return errors.Wrap(e, "copy file")
	}
	if err = s.fsEval.Symlink(target, dest); err != nil {
		return
	}
	err = s.restoreMetadata(dest, si)
	return
}

func (s *FileSystemBuilder) copyFile(src string, si os.FileInfo, dest string) (err error) {
	s.logCopy(src, dest, si.Mode())
	var srcFile, destFile *os.File
	if srcFile, err = os.Open(src); err != nil {
		return
	}
	defer srcFile.Close()
	if err = s.createAllDirs(filepath.Dir(src), filepath.Dir(dest)); err != nil {
		return
	}
	if e := s.fsEval.RemoveAll(dest); e != nil {
		return
	}
	// TODO: use fseval to copy file metadata
	if destFile, err = s.fsEval.Create(dest); err != nil {
		return
	}
	defer destFile.Close()
	if _, err = io.Copy(destFile, srcFile); err != nil {
		return errors.Wrapf(err, "copy %s => %s", src, dest)
	}
	if err = s.restoreMetadata(dest, si); err != nil {
		return
	}
	return destFile.Sync()
}

func (s *FileSystemBuilder) createAllDirs(src, dest string) (err error) {
	if s.dirs[dest] {
		return
	}
	si, err := os.Stat(src)
	if err != nil {
		return
	}
	if di, e := os.Stat(dest); e == nil {
		if di.IsDir() {
			if err = checkWithin(dest, s.root); err != nil {
				return
			}
			s.dirs[dest] = true
			return
		} else {
			// Remove file if it is no directory
			if err = s.fsEval.Remove(dest); err != nil {
				return
			}
		}
	} else if os.IsNotExist(e) {
		psrc := filepath.Dir(src)
		pdest := filepath.Dir(dest)
		if err = s.createAllDirs(psrc, pdest); err != nil {
			return
		}
	} else {
		return e
	}
	if err = checkWithin(dest, s.root); err != nil {
		return
	}
	s.logCopy(src, dest, si.Mode())
	if err = s.fsEval.Mkdir(dest, si.Mode()); err != nil {
		return
	}
	err = s.restoreMetadata(dest, si)
	s.dirs[dest] = true
	return
}

func (s *FileSystemBuilder) logCopy(src, dest string, mode os.FileMode) {
	dest, err := filepath.Rel(s.root, dest)
	if err != nil {
		panic(err)
	}
	dest = "/" + dest
	s.log.Printf("%s %s => %s", mode, src, dest)
}

func (s *FileSystemBuilder) restoreMetadata(path string, meta os.FileInfo) (err error) {
	st := meta.Sys().(*syscall.Stat_t)
	atime, mtime := fileTime(meta)
	if mtime.After(s.latestMtime) {
		s.latestMtime = mtime
	}
	if meta.Mode()&os.ModeSymlink == 0 {
		if err = s.fsEval.Chmod(path, meta.Mode()); err != nil {
			return errors.Wrap(err, "restore mode")
		}
	}
	// TODO: use fseval method if available
	if !s.rootless {
		if err = errors.Wrap(os.Lchown(path, int(st.Uid), int(st.Gid)), "chown"); err != nil {
			return
		}
	}
	xattrs, err := s.fsEval.Llistxattr(path)
	if err != nil {
		return
	}
	if err = s.fsEval.Lclearxattrs(path); err != nil {
		return errors.Wrapf(err, "clear xattrs: %s", path)
	}
	for _, name := range xattrs {
		value, e := s.fsEval.Lgetxattr(path, name)
		if err != nil {
			return e
		}
		if err = s.fsEval.Lsetxattr(path, name, value, 0); err != nil {
			// In rootless mode, some xattrs will fail (security.capability).
			// This is _fine_ as long as not run as root
			if s.rootless && os.IsPermission(errors.Cause(err)) {
				s.log.Printf("restore metadata: ignoring EPERM on setxattr %s: %v", name, err)
				continue
			}
			return errors.Wrapf(err, "set xattr: %s", path)
		}
	}
	return s.restoreTimeMetadata(path, atime, mtime)
}

func (s *FileSystemBuilder) restoreTimeMetadata(path string, atime, mtime time.Time) error {
	err := s.fsEval.Lutimes(path, atime, mtime)
	return errors.Wrapf(err, "restore file times: %s", path)
}

func checkWithin(file, rootDir string) error {
	file, err := realPath(file)
	if err == nil && !within(file, rootDir) {
		err = errors.Errorf("path %q is outside rootfs", file)
	}
	return err
}

func realPath(file string) (f string, err error) {
	f, err = filepath.EvalSymlinks(file)
	if err != nil && os.IsNotExist(err) {
		fileName := filepath.Base(file)
		file, err = realPath(filepath.Dir(file))
		f = filepath.Join(file, fileName)
	}
	return
}

func fileTime(st os.FileInfo) (atime, mtime time.Time) {
	stu := st.Sys().(*syscall.Stat_t)
	atime = time.Unix(int64(stu.Atim.Sec), int64(stu.Atim.Nsec))
	mtime = st.ModTime()
	return
}
