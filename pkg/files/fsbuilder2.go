package files

import (
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/mgoltzsche/cntnr/pkg/idutils"
	"github.com/openSUSE/umoci/pkg/fseval"
	"github.com/opencontainers/go-digest"
	"github.com/pkg/errors"
)

type FSOptions struct {
	Rootless   bool
	IdMappings idutils.IdMappings
	FsEval     fseval.FsEval
}

func NewFSOptions(rootless bool) FSOptions {
	idMap := idutils.MapIdentity
	fsEval := fseval.DefaultFsEval
	if rootless {
		idMap = idutils.MapRootless
		fsEval = fseval.RootlessFsEval
	}
	return FSOptions{rootless, idMap, fsEval}
}

type FsBuilder struct {
	root    *FsNode
	fsEval  fseval.FsEval
	sources *Sources
	err     error
}

func NewFsBuilder(opts FSOptions) *FsBuilder {
	fsEval := fseval.DefaultFsEval
	if opts.Rootless {
		fsEval = fseval.RootlessFsEval
	}
	return &FsBuilder{
		root:    NewFS(),
		fsEval:  fsEval,
		sources: NewSources(fsEval, opts.IdMappings),
	}
}

func (b *FsBuilder) Hash() (d digest.Digest, err error) {
	if b.err != nil {
		return d, b.err
	}
	return b.root.Hash()
}

func (b *FsBuilder) Write(w Writer) error {
	if b.err != nil {
		return b.err
	}
	return b.root.WriteFiles(w)
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
			if dest, b.err = destFilePath(src, dest); b.err == nil {
				b.AddFiles(src, dest, usr)
			}
		} else {
			// sources from glob pattern
			src = filepath.Join(srcfs, src)
			matches, err := filepath.Glob(src)
			if err != nil {
				b.err = err
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

func (b *FsBuilder) AddFiles(srcFile, dest string, usr *idutils.UserIds) {
	if b.err != nil {
		return
	}
	fi, err := b.fsEval.Lstat(srcFile)
	if err != nil {
		b.err = errors.Wrap(err, "add")
		return
	}
	if fi.IsDir() {
		b.err = b.addDirContents(srcFile, dest, b.root, usr)
	} else {
		src, err := b.sources.FileOverlay(srcFile, fi, usr)
		if err != nil {
			b.err = err
			return
		}
		if src.Type() == TypeFile {
			if dest, b.err = destFilePath(srcFile, dest); b.err != nil {
				return
			}
		}
		_, b.err = b.root.Add(src, dest)
	}
}

// Adds file/directory recursively
func (b *FsBuilder) addFiles(file, dest string, parent *FsNode, fi os.FileInfo, usr *idutils.UserIds) (err error) {
	src, err := b.sources.File(file, fi, usr)
	if err != nil {
		return
	}
	parent, err = parent.Add(src, dest)
	if err != nil {
		return
	}
	if fi.Mode().IsDir() {
		err = b.addDirContents(file, dest, parent, usr)
	}
	return
}

// Adds directory contents recursively
func (b *FsBuilder) addDirContents(dir, dest string, parent *FsNode, usr *idutils.UserIds) (err error) {
	files, err := b.fsEval.Readdir(dir)
	if err != nil {
		return errors.New(err.Error())
	}
	for _, f := range files {
		childSrc := filepath.Join(dir, f.Name())
		if err = b.addFiles(childSrc, f.Name(), parent, f, usr); err != nil {
			return
		}
	}
	return
}

func secureSourcePath(root, file string) (err error) {
	dir, file := filepath.Split(file)
	if dir, err = filepath.EvalSymlinks(dir); err != nil {
		return errors.Wrap(err, "secure source")
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
