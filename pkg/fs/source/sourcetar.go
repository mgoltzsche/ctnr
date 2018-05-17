package source

import (
	"archive/tar"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/mgoltzsche/cntnr/pkg/fs"
	"github.com/mgoltzsche/cntnr/pkg/idutils"
	"github.com/openSUSE/umoci/oci/layer"
	"github.com/opencontainers/go-digest"
	"github.com/pkg/errors"
)

var (
	dirAttrs               = fs.FileAttrs{Mode: os.ModeDir | 0755}
	_        fs.BlobSource = &sourceTar{}
)

type sourceTar struct {
	file string
	hash string
}

func NewSourceTar(file string) *sourceTar {
	return &sourceTar{file, ""}
}

func (s *sourceTar) Equal(o fs.Source) (bool, error) {
	return false, nil
}

func (s *sourceTar) Attrs() fs.NodeInfo {
	return fs.NodeInfo{fs.TypeOverlay, fs.FileAttrs{Mode: os.ModeDir | 0755}}
}

func (s *sourceTar) HashIfAvailable() string {
	return s.hash
}

func (s *sourceTar) Hash() (string, error) {
	if s.hash == "" {
		f, err := os.Open(s.file)
		if err != nil {
			return "", errors.Errorf("hash: %s", err)
		}
		defer f.Close()
		d, err := digest.FromReader(f)
		if err != nil {
			return "", errors.Errorf("hash %s: %s", s.file, err)
		}
		s.hash = d.String()
	}
	return s.hash, nil
}

func (s *sourceTar) DerivedAttrs() (a fs.NodeAttrs, err error) {
	a.Hash, err = s.Hash()
	return
}

func (s *sourceTar) Write(dest, name string, w fs.Writer, written map[fs.Source]string) error {
	return w.Lazy(dest, name, s, written)
}

func (s *sourceTar) Expand(dest string, w fs.Writer, written map[fs.Source]string) (err error) {
	f, err := os.Open(s.file)
	if err != nil {
		return errors.Wrap(err, "extract tar")
	}
	defer f.Close()
	if err = unpackTar(f, dest, w); err != nil {
		return errors.Wrap(err, "extract tar")
	}
	return
}

func unpackTar(r io.Reader, dest string, w fs.Writer) error {
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return errors.Wrap(err, "read next tar entry")
		}
		links := map[string]string{}
		if err = unpackTarEntry(hdr, tr, dest, w, links); err != nil {
			return errors.Wrapf(err, "unpack tar entry: %s", hdr.Name)
		}
		for path, target := range links {
			if err = w.Link(path, target); err != nil {
				return errors.Wrapf(err, "unpack tar entry link: %s", hdr.Name)
			}
		}
	}
	return nil
}

// Derived from umoci's tar_extract.go to allow separate source/dest interfaces
// and filter archive contents on extraction
func unpackTarEntry(hdr *tar.Header, r io.Reader, dest string, w fs.Writer, links map[string]string) (err error) {
	path := layer.CleanPath(filepath.Join(dest, hdr.Name))
	dir, file := filepath.Split(path)

	// Remove file if whiteout
	if strings.HasPrefix(file, fs.WhiteoutPrefix) {
		file = strings.TrimPrefix(file, fs.WhiteoutPrefix)
		return w.Remove(filepath.Join(dir, file))
	}

	// Convert attributes
	fi := hdr.FileInfo()
	a := fs.FileAttrs{
		Mode:    fi.Mode(),
		UserIds: idutils.UserIds{uint(hdr.Uid), uint(hdr.Gid)},
		FileTimes: fs.FileTimes{
			Atime: hdr.AccessTime,
			Mtime: hdr.ModTime,
		},
		Xattrs: hdr.Xattrs,
	}

	// Write file
	switch hdr.Typeflag {
	// regular file
	case tar.TypeReg, tar.TypeRegA:
		delete(links, path)
		a.Size = hdr.Size
		_, err = w.File(path, NewSourceFile(fs.NewReadable(r), a))
	// directory
	case tar.TypeDir:
		delete(links, path)
		err = w.Dir(path, filepath.Base(path), a)
	// hard link
	case tar.TypeLink:
		links[path] = filepath.Join(string(filepath.Separator)+dest, hdr.Linkname)
	// symbolic link
	case tar.TypeSymlink:
		a.Symlink = hdr.Linkname
		if filepath.IsAbs(a.Symlink) {
			a.Symlink = filepath.Join(string(filepath.Separator)+dest, a.Symlink)
		}
		delete(links, path)
		err = w.Symlink(path, a)
	// character device node, block device node
	case tar.TypeChar, tar.TypeBlock:
		delete(links, path)
		err = w.Device(path, fs.DeviceAttrs{a, hdr.Devmajor, hdr.Devminor})
	// fifo node
	case tar.TypeFifo:
		delete(links, path)
		err = w.Fifo(path, fs.DeviceAttrs{a, hdr.Devmajor, hdr.Devminor})
	default:
		err = errors.Errorf("unpack entry: %s: unknown typeflag '\\x%x'", hdr.Name, hdr.Typeflag)
	}
	return
}
