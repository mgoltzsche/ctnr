package tree

import (
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/mgoltzsche/cntnr/pkg/fs"
	"github.com/mgoltzsche/cntnr/pkg/fs/source"
	"github.com/mgoltzsche/cntnr/pkg/idutils"
	"github.com/openSUSE/umoci/pkg/fseval"
	"github.com/opencontainers/go-digest"
	"github.com/pkg/errors"
)

type FsBuilder struct {
	fs      fs.FsNode
	fsEval  fseval.FsEval
	sources *source.Sources
	err     error
}

func FromDir(rootfs string, rootless bool) (fs.FsNode, error) {
	b := NewFsBuilder(NewFS(), fs.NewFSOptions(rootless))
	b.AddDir(rootfs, "/", nil)
	return b.FS()
}

func NewFsBuilder(rootfs fs.FsNode, opts fs.FSOptions) *FsBuilder {
	fsEval := fseval.DefaultFsEval
	var attrMapper fs.AttrMapper
	if opts.Rootless {
		fsEval = fseval.RootlessFsEval
		attrMapper = fs.NewRootlessAttrMapper(opts.IdMappings)
	} else {
		attrMapper = fs.NewAttrMapper(opts.IdMappings)
	}
	return &FsBuilder{
		fs:      rootfs,
		fsEval:  fsEval,
		sources: source.NewSources(fsEval, attrMapper),
	}
}

func (b *FsBuilder) FS() (fs.FsNode, error) {
	return b.fs, errors.Wrap(b.err, "fsbuilder")
}

func (b *FsBuilder) Hash(attrs fs.AttrSet) (d digest.Digest, err error) {
	if b.err != nil {
		return d, errors.Wrap(b.err, "fsbuilder")
	}
	return b.fs.Hash(attrs)
}

func (b *FsBuilder) Write(w fs.Writer) error {
	if b.err != nil {
		return errors.Wrap(b.err, "fsbuilder")
	}
	return b.fs.Write(w)
}

func (b *FsBuilder) AddAll(srcfs string, sources []string, dest string, usr *idutils.UserIds) {
	if b.err != nil {
		return
	}
	if len(sources) > 1 {
		dest = filepath.Clean(dest) + string(filepath.Separator)
	}
	for _, src := range sources {
		if isUrl(src) {
			// source from URL
			// TODO: add url
			panic("TODO: support URL")
		} else {
			// sources from glob pattern
			if !filepath.IsAbs(src) {
				src = filepath.Join(srcfs, src)
			}
			matches, err := filepath.Glob(src)
			if err != nil {
				b.err = errors.Wrap(err, "source file pattern")
				return
			}
			if len(matches) == 0 {
				b.err = errors.Errorf("source pattern %q does not match any files", src)
				return
			}
			if len(matches) > 1 {
				dest = filepath.Clean(dest) + string(filepath.Separator)
			}
			for _, file := range matches {
				if b.err = secureSourcePath(srcfs, file); b.err != nil {
					return
				}
				b.AddFiles(file, dest, usr)
			}
		}
	}
}

func (b *FsBuilder) AddDir(srcFile, dest string, usr *idutils.UserIds) {
	if b.err != nil {
		return
	}
	fi, err := b.fsEval.Lstat(srcFile)
	if err != nil {
		b.err = errors.WithMessage(err, "add")
		return
	}
	_, err = b.addFiles(srcFile, dest, b.fs, fi, usr)
	b.err = errors.WithMessage(err, "add")
}

func (b *FsBuilder) AddFiles(srcFile, dest string, usr *idutils.UserIds) {
	if b.err != nil {
		return
	}
	fi, err := b.fsEval.Lstat(srcFile)
	if err != nil {
		b.err = errors.WithMessage(err, "add")
		return
	}
	if fi.IsDir() {
		parent, err := b.fs.Mkdirs(dest)
		if err != nil {
			b.err = errors.WithMessage(err, "add")
			return
		}
		b.err = b.addDirContents(srcFile, dest, parent, usr)
	} else {
		src, err := b.sources.FileOverlay(srcFile, fi, usr)
		if err != nil {
			b.err = err
			return
		}
		t := src.Attrs().NodeType
		if t != fs.TypeDir && t != fs.TypeOverlay {
			// append source base name to dest if dest ends with /
			if dest, b.err = destFilePath(srcFile, dest); b.err != nil {
				return
			}
		}
		_, b.err = b.fs.AddUpper(dest, src)
	}
}

// Adds file/directory recursively
func (b *FsBuilder) addFiles(file, dest string, parent fs.FsNode, fi os.FileInfo, usr *idutils.UserIds) (r fs.FsNode, err error) {
	src, err := b.sources.File(file, fi, usr)
	if err != nil {
		return
	}
	if src == nil || src.Attrs().NodeType == "" {
		panic("no source returned or empty node type received from source")
	}
	r, err = parent.AddUpper(dest, src)
	if err != nil {
		return
	}
	if src.Attrs().NodeType == fs.TypeDir {
		err = b.addDirContents(file, dest, r, usr)
	}
	return
}

// Adds directory contents recursively
func (b *FsBuilder) addDirContents(dir, dest string, parent fs.FsNode, usr *idutils.UserIds) (err error) {
	files, err := b.fsEval.Readdir(dir)
	if err != nil {
		return errors.New(err.Error())
	}
	for _, f := range files {
		childSrc := filepath.Join(dir, f.Name())
		if _, err = b.addFiles(childSrc, f.Name(), parent, f, usr); err != nil {
			return
		}
	}
	return
}

func secureSourcePath(root, file string) (err error) {
	dir, file := filepath.Split(file)
	if dir, err = filepath.EvalSymlinks(dir); err != nil {
		return errors.WithMessage(err, "secure source")
	}
	file = filepath.Join(dir, file)
	if !filepath.HasPrefix(file, root) {
		err = errors.Errorf("secure source: source file %s is outside context directory", file)
	}
	return
}

func destFilePath(src string, dest string) (string, error) {
	if strings.HasSuffix(dest, "/") {
		fileName := path.Base(filepath.ToSlash(src))
		if fileName == "" {
			return "", errors.Errorf("cannot derive file name for destination %q from source %q. Please specify file name within destination!", dest, src)
		}
		return filepath.Join(dest, fileName), nil
	}
	return dest, nil
}

func isUrl(v string) bool {
	v = strings.ToLower(v)
	return strings.HasPrefix(v, "https://") || strings.HasPrefix(v, "http://")
}
