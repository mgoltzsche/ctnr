package files

import (
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/mgoltzsche/cntnr/pkg/idutils"
	"github.com/openSUSE/umoci/pkg/fseval"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/pkg/errors"
)

type Logger interface {
	Printf(fmt string, o ...interface{})
}

type FSOptions struct {
	UIDMappings []specs.LinuxIDMapping
	GIDMappings []specs.LinuxIDMapping
	Rootless    bool
}

type FileSystemBuilder struct {
	root        string
	dirs        map[string]bool
	files       []string
	latestMtime time.Time
	fsEval      fseval.FsEval
	rootless    bool
	uidMappings []specs.LinuxIDMapping
	gidMappings []specs.LinuxIDMapping
	log         Logger
}

func NewFileSystemBuilder(root string, opts FSOptions, logger Logger) *FileSystemBuilder {
	fsEval := fseval.DefaultFsEval
	if opts.Rootless {
		fsEval = fseval.RootlessFsEval
	}
	return &FileSystemBuilder{
		root:        filepath.Clean(root),
		dirs:        map[string]bool{},
		files:       []string{},
		fsEval:      fsEval,
		rootless:    opts.Rootless,
		uidMappings: opts.UIDMappings,
		gidMappings: opts.GIDMappings,
		log:         logger,
	}
}

func (s *FileSystemBuilder) Files() []string {
	return s.files
}

func (s *FileSystemBuilder) AddAll(srcfs string, pattern []string, dest string, usr *idutils.UserIds) (err error) {
	srcFiles, err := Glob(srcfs, pattern)
	if err != nil {
		return
	}
	cpPairs := Map(srcFiles, dest)
	for _, p := range cpPairs {
		if err = s.Add(p.Source, p.Dest, usr); err != nil {
			return
		}
	}
	return
}

func (s *FileSystemBuilder) Add(src, destFile string, usr *idutils.UserIds) (err error) {
	if s.rootless && usr != nil && (usr.Uid != 0 || usr.Gid != 0) {
		return errors.Errorf("in rootless mode only own user ID can be used but %s provided", usr.String())
	}
	if usr != nil {
		// Map user to host
		var u idutils.UserIds
		if u, err = usr.ToHost(s.uidMappings, s.gidMappings); err != nil {
			return errors.Wrap(err, "add")
		}
		usr = &u
	}
	src = filepath.Clean(src)
	destFile = filepath.Clean(destFile)
	destDir := filepath.Dir(filepath.Join(s.root, destFile))
	st, err := s.fsEval.Lstat(src)
	if err != nil {
		return errors.Wrap(err, "add")
	}
	if !s.dirs[destDir] && within(destDir, s.root) {
		stu := st.Sys().(*syscall.Stat_t)
		atime := time.Unix(int64(stu.Atim.Sec), int64(stu.Atim.Nsec))
		mtime := st.ModTime()
		if err = s.mkdirAll(filepath.Dir(destFile), 0755, atime, mtime); err != nil {
			return errors.Wrap(err, "add")
		}
		s.dirs[destDir] = true
	}
	if err = s.copy(src, st, destFile, usr); err == nil {
		err = s.restoreTimeMetadata(s.root, s.latestMtime, s.latestMtime)
	}
	return errors.Wrap(err, "add")
}

// Recursively creates directories if they do not yet exist applying the provided mode, atime, mtime
func (s *FileSystemBuilder) mkdirAll(path string, mode os.FileMode, atime, mtime time.Time) (err error) {
	path = filepath.Clean(string(filepath.Separator) + path)
	absPath := filepath.Join(s.root, path)
	st, err := os.Stat(absPath)
	if err == nil {
		if st.IsDir() {
			return
		} else {
			if err = s.fsEval.Remove(absPath); err != nil {
				return
			}
		}
	} else if !os.IsNotExist(err) {
		return
	}
	if err = s.mkdirAll(filepath.Dir(path), mode, atime, mtime); err != nil {
		return
	}
	if err = s.fsEval.Mkdir(absPath, mode); err != nil {
		return
	}
	if err = s.restoreTimeMetadata(absPath, atime, mtime); err == nil {
		s.files = append(s.files, path)
	}
	return
}

func (s *FileSystemBuilder) copy(src string, si os.FileInfo, dest string, usr *idutils.UserIds) (err error) {
	if err = checkWithin(filepath.Join(s.root, dest), s.root); err != nil {
		return
	}
	dest = filepath.Clean(string(filepath.Separator) + dest)
	switch {
	case si.IsDir():
		return s.copyDir(src, dest, usr)
	case si.Mode()&os.ModeSymlink != 0:
		return s.copyLink(src, si, dest, usr)
	default:
		return s.copyFile(src, si, dest, usr)
	}
	return
}

func (s *FileSystemBuilder) copyDir(src, dest string, usr *idutils.UserIds) (err error) {
	if err = s.createAllDirs(src, dest, usr); err != nil {
		return
	}
	files, err := ioutil.ReadDir(src)
	if err != nil {
		return
	}
	for _, f := range files {
		if err = s.copy(filepath.Join(src, f.Name()), f, filepath.Join(dest, f.Name()), usr); err != nil {
			return
		}
	}
	return
}

func (s *FileSystemBuilder) copyLink(src string, si os.FileInfo, dest string, usr *idutils.UserIds) (err error) {
	absDest := filepath.Join(s.root, dest)
	target, err := s.fsEval.Readlink(src)
	if err != nil {
		return
	}
	target = filepath.Clean(target)
	if !filepath.IsAbs(target) && !within(filepath.Join(filepath.Dir(absDest), target), s.root) {
		return errors.Errorf("link %s target %q outside root directory", absDest, target)
	}
	if err = s.createAllDirs(filepath.Dir(src), filepath.Dir(dest), usr); err != nil {
		return
	}
	if e := s.fsEval.RemoveAll(absDest); e != nil {
		return errors.Wrap(e, "copy file")
	}
	if err = s.fsEval.Symlink(target, absDest); err != nil {
		return
	}
	if err = s.restoreMetadata(absDest, si, usr); err == nil {
		s.logCopy(src, dest, 0) // TODO: set correct link file mode
	}
	return
}

func (s *FileSystemBuilder) copyFile(src string, si os.FileInfo, dest string, usr *idutils.UserIds) (err error) {
	absDest := filepath.Join(s.root, dest)
	var srcFile, destFile *os.File
	if srcFile, err = os.Open(src); err != nil {
		return
	}
	defer srcFile.Close()
	if err = s.createAllDirs(filepath.Dir(src), filepath.Dir(dest), usr); err != nil {
		return
	}
	if e := s.fsEval.RemoveAll(absDest); e != nil && !os.IsNotExist(e) {
		return e
	}
	if destFile, err = s.fsEval.Create(absDest); err != nil {
		return
	}
	defer destFile.Close()
	if _, err = io.Copy(destFile, srcFile); err != nil {
		return errors.Wrapf(err, "copy %s => %s", src, absDest)
	}
	if err = s.restoreMetadata(absDest, si, usr); err == nil {
		s.logCopy(src, dest, si.Mode())
	}
	return
}

func (s *FileSystemBuilder) createAllDirs(src, dest string, usr *idutils.UserIds) (err error) {
	absDest := filepath.Join(s.root, dest)
	if s.dirs[absDest] {
		return
	}
	si, err := os.Stat(src)
	if err != nil {
		return
	}
	if di, e := os.Stat(absDest); e == nil {
		if di.IsDir() {
			if err = checkWithin(absDest, s.root); err != nil {
				return
			}
			s.dirs[absDest] = true
			return
		} else {
			// Remove file if it is no directory
			if err = s.fsEval.Remove(absDest); err != nil {
				return
			}
		}
	} else if os.IsNotExist(e) {
		psrc := filepath.Dir(src)
		pdest := filepath.Dir(dest)
		if err = s.createAllDirs(psrc, pdest, usr); err != nil {
			return
		}
	} else {
		return e
	}
	if err = checkWithin(absDest, s.root); err != nil {
		return
	}
	if err = s.fsEval.Mkdir(absDest, si.Mode()); err != nil {
		return
	}
	if err = s.restoreMetadata(absDest, si, usr); err == nil {
		s.dirs[absDest] = true
		s.logCopy(src, dest, si.Mode())
	}
	return
}

func (s *FileSystemBuilder) logCopy(src, dest string, mode os.FileMode) {
	s.files = append(s.files, dest)
	s.log.Printf("%s %s => %s", mode, src, dest)
}

func (s *FileSystemBuilder) restoreMetadata(path string, meta os.FileInfo, usr *idutils.UserIds) (err error) {
	st := meta.Sys().(*syscall.Stat_t)
	atime, mtime := fileTime(meta)
	if mtime.After(s.latestMtime) {
		s.latestMtime = mtime
	}

	// chmod
	if meta.Mode()&os.ModeSymlink == 0 {
		if err = s.fsEval.Chmod(path, meta.Mode()); err != nil {
			return errors.Wrap(err, "restore mode")
		}
	}

	// chown
	if !s.rootless {
		// TODO: use fseval method if available
		uid := int(st.Uid)
		gid := int(st.Gid)
		if usr != nil {
			uid = int(usr.Uid)
			gid = int(usr.Gid)
		}
		if err = errors.Wrap(os.Lchown(path, uid, gid), "chown"); err != nil {
			return
		}
	}

	// xattrs
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
