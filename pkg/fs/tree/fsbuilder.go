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
	b.CopyDir(rootfs, "/", nil)
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

type fileSourceFactory func(file string, fi os.FileInfo, usr *idutils.UserIds) (fs.Source, error)

func (b *FsBuilder) createFile(file string, fi os.FileInfo, usr *idutils.UserIds) (fs.Source, error) {
	return b.sources.File(file, fi, usr)
}

func (b *FsBuilder) createOverlayOrFile(file string, fi os.FileInfo, usr *idutils.UserIds) (fs.Source, error) {
	return b.sources.FileOverlay(file, fi, usr)
}

// Copies all files that match the provided glob source pattern.
// Source tar archives are extracted into dest.
// Source URLs are also supported.
// See https://docs.docker.com/engine/reference/builder/#add
func (b *FsBuilder) AddAll(srcfs string, sources []string, dest string, usr *idutils.UserIds) {
	if b.err != nil {
		return
	}
	if len(sources) == 0 {
		b.err = errors.New("add: no source provided")
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
			if err := b.copy(srcfs, src, dest, usr, b.createOverlayOrFile); err != nil {
				b.err = errors.Wrap(err, "add "+src)
				return
			}
		}
	}
}

// Copies all files that match the provided glob source pattern to dest.
// See https://docs.docker.com/engine/reference/builder/#copy
func (b *FsBuilder) CopyAll(srcfs string, sources []string, dest string, usr *idutils.UserIds) {
	if b.err != nil {
		return
	}
	if len(sources) == 0 {
		b.err = errors.New("copy: no source provided")
		return
	}
	if len(sources) > 1 {
		dest = filepath.Clean(dest) + string(filepath.Separator)
	}
	for _, src := range sources {
		if err := b.copy(srcfs, src, dest, usr, b.createOverlayOrFile); err != nil {
			b.err = errors.Wrap(err, "copy "+src)
			return
		}
	}
}

func (b *FsBuilder) copy(srcfs, src, dest string, usr *idutils.UserIds, factory fileSourceFactory) (err error) {
	// sources from glob pattern
	src = filepath.Join(srcfs, src)
	matches, err := filepath.Glob(src)
	if err != nil {
		return errors.Wrap(err, "source file pattern")
	}
	if len(matches) == 0 {
		return errors.Errorf("source pattern %q does not match any files", src)
	}
	if len(matches) > 1 {
		dest = filepath.Clean(dest) + string(filepath.Separator)
	}
	for _, file := range matches {
		if file, err = secureSourceFile(srcfs, file); err != nil {
			return
		}
		if err = b.addFiles(file, dest, usr, factory); err != nil {
			return
		}
	}
	return
}

func (b *FsBuilder) AddFiles(srcFile, dest string, usr *idutils.UserIds) {
	if b.err != nil {
		return
	}
	if err := b.addFiles(srcFile, dest, usr, b.createFile); err != nil {
		b.err = err
	}
}

func (b *FsBuilder) addFiles(srcFile, dest string, usr *idutils.UserIds, factory fileSourceFactory) (err error) {
	fi, err := b.fsEval.Lstat(srcFile)
	if err != nil {
		return
	}
	if fi.IsDir() {
		var parent fs.FsNode
		if parent, err = b.fs.Mkdirs(dest); err != nil {
			return
		}
		err = b.copyDirContents(srcFile, dest, parent, usr)
	} else {
		var src fs.Source
		if src, err = factory(srcFile, fi, usr); err != nil {
			return
		}
		t := src.Attrs().NodeType
		if t != fs.TypeDir && t != fs.TypeOverlay {
			// append source base name to dest if dest ends with /
			if dest, err = destFilePath(srcFile, dest); err != nil {
				return
			}
		}
		_, err = b.fs.AddUpper(dest, src)
	}
	return
}

// Copies the directory recursively including the directory itself.
func (b *FsBuilder) CopyDir(srcFile, dest string, usr *idutils.UserIds) {
	if b.err != nil {
		return
	}
	fi, err := b.fsEval.Lstat(srcFile)
	if err != nil {
		b.err = errors.WithMessage(err, "add")
		return
	}
	_, err = b.copyFiles(srcFile, dest, b.fs, fi, usr)
	b.err = errors.WithMessage(err, "add")
}

// Adds file/directory recursively
func (b *FsBuilder) copyFiles(file, dest string, parent fs.FsNode, fi os.FileInfo, usr *idutils.UserIds) (r fs.FsNode, err error) {
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
		err = b.copyDirContents(file, dest, r, usr)
	}
	return
}

// Adds directory contents recursively
func (b *FsBuilder) copyDirContents(dir, dest string, parent fs.FsNode, usr *idutils.UserIds) (err error) {
	files, err := b.fsEval.Readdir(dir)
	if err != nil {
		return errors.New(err.Error())
	}
	for _, f := range files {
		childSrc := filepath.Join(dir, f.Name())
		if _, err = b.copyFiles(childSrc, f.Name(), parent, f, usr); err != nil {
			return
		}
	}
	return
}

func secureSourceFile(root, file string) (f string, err error) {
	// TODO: use fseval
	if f, err = filepath.EvalSymlinks(file); err != nil {
		return "", errors.Wrap(err, "secure source")
	}
	if !filepath.HasPrefix(f, root) {
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
