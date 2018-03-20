package builder

import (
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/pkg/errors"
)

type Logger interface {
	Printf(fmt string, o ...interface{})
}

type FileSystemBuilder struct {
	root string
	dirs map[string]bool
	log  Logger
}

func NewFileSystemBuilder(root string, logger Logger) *FileSystemBuilder {
	return &FileSystemBuilder{
		root: filepath.Clean(root),
		dirs: map[string]bool{},
		log:  logger,
	}
}

func (s *FileSystemBuilder) Add(root string, srcPattern []string, dest string) (err error) {
	destc := filepath.Clean(dest)
	if strings.Index(destc, "../") == 0 || strings.Index(destc, "/../") == 0 || destc == ".." || destc == "/.." {
		return errors.Errorf("destination %q is outside root directory", dest)
	}

	files, err := resolveFilePatterns(root, srcPattern)
	if err == nil {
		// TODO: eventually sort file so that dirs come first to make sure their file attributes get applied properly (e.g. check if Glob() output requires parent dir entry extraction, sort by depth)
		for _, file := range files {
			destDir := dest
			destFile := filepath.Base(file)
			if len(files) == 1 && len(dest) > 0 && destDir[len(dest)-1] != '/' {
				// Use dest as file name without appending src file name
				// if there is only one source file and dest does not end with '/'
				destDir = filepath.Dir(dest)
				destFile = filepath.Base(dest)
			}
			if err = s.add(file, destDir, destFile); err != nil {
				break
			}
		}
	}
	return errors.Wrap(err, "add")
}

func (s *FileSystemBuilder) add(src, destDir, destFile string) (err error) {
	src = filepath.Clean(src)
	destDir = filepath.Join(s.root, destDir)
	destFile = filepath.Join(destDir, destFile)
	si, err := os.Lstat(src)
	if err != nil {
		return
	}
	if !s.dirs[destDir] {
		if err = os.MkdirAll(destDir, 0755); err != nil {
			return
		}
		s.dirs[destDir] = true
	}
	return s.copy(src, si, destFile)
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
	target, err := os.Readlink(src)
	if err != nil {
		return
	}
	target = filepath.Clean(target)
	if !filepath.IsAbs(target) && !s.isWithinRoot(filepath.Join(filepath.Dir(dest), target)) {
		return errors.Errorf("link %s target %q outside root directory", dest, target)
	}
	s.log.Printf("l    %s => %s (%s)", src, dest, target)
	if err = s.createAllDirs(filepath.Dir(src), filepath.Dir(dest)); err != nil {
		return
	}
	if e := os.Remove(dest); e != nil && !os.IsNotExist(e) {
		return e
	}
	if err = os.Symlink(target, dest); err != nil {
		return
	}
	if st, ok := si.Sys().(*syscall.Stat_t); ok {
		err = os.Lchown(dest, int(st.Uid), int(st.Gid))
	}
	return
}

func (s *FileSystemBuilder) checkWithinRoot(file string) error {
	file, err := realPath(file)
	if err == nil && !s.isWithinRoot(file) {
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

func (s *FileSystemBuilder) isWithinRoot(file string) bool {
	a := string(filepath.Separator)
	return strings.Index(file+a, s.root+a) == 0
}

func (s *FileSystemBuilder) copyFile(src string, si os.FileInfo, dest string) (err error) {
	s.log.Printf("f %d %s => %s", si.Mode(), src, dest)
	var srcFile, destFile *os.File
	if srcFile, err = os.Open(src); err != nil {
		return
	}
	defer srcFile.Close()
	if err = s.createAllDirs(filepath.Dir(src), filepath.Dir(dest)); err != nil {
		return
	}
	if e := os.Remove(dest); e != nil && !os.IsNotExist(e) {
		return e
	}
	if destFile, err = os.Create(dest); err != nil {
		return
	}
	defer destFile.Close()
	if _, err = io.Copy(destFile, srcFile); err != nil {
		return
	}
	if err = destFile.Sync(); err != nil {
		return
	}
	if err = os.Chmod(dest, si.Mode()); err != nil {
		return
	}
	return chown(dest, si)
}

func (s *FileSystemBuilder) createAllDirs(src, dest string) (err error) {
	if s.dirs[dest] {
		return
	}
	if dest == s.root || src == string(filepath.Separator) {
		if err = s.checkWithinRoot(dest); err != nil {
			return
		}
		err = os.MkdirAll(dest, 0755)
		s.dirs[dest] = true
		return
	}
	si, err := os.Stat(src)
	if err != nil {
		return
	}
	if si, e := os.Stat(dest); e == nil {
		if si.IsDir() {
			if err = s.checkWithinRoot(dest); err != nil {
				return
			}
			s.dirs[dest] = true
			return
		} else {
			// Remove file if it is no directory
			if err = os.Remove(dest); err != nil {
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
	if err = s.checkWithinRoot(dest); err != nil {
		return
	}
	s.log.Printf("d %d %s => %s", si.Mode(), src, dest)
	if err = os.Mkdir(dest, si.Mode()); err != nil {
		return
	}
	err = chown(dest, si)
	s.dirs[dest] = true
	return
}

func chown(file string, info os.FileInfo) (err error) {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		err = os.Chown(file, int(st.Uid), int(st.Gid))
	}
	return
}

func resolveFilePatterns(root string, pattern []string) (files []string, err error) {
	for _, p := range pattern {
		if _, err = filepath.Match(p, "/"); err != nil {
			return
		}
	}
	for _, p := range pattern {
		if strings.Index(filepath.Clean("/"+p)+"/", root+"/") != 0 {
			p = filepath.Join(root, p)
		}
		f, e := filepath.Glob(p)
		if e != nil {
			return files, e
		}
		if len(f) == 0 {
			return files, errors.Errorf("file pattern %q did not match", p)
		}
		files = append(files, f...)
	}
	return
}
