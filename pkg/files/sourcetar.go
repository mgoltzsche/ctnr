package files

import (
	"archive/tar"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/mgoltzsche/cntnr/pkg/idutils"
	"github.com/openSUSE/umoci/oci/layer"
	"github.com/opencontainers/go-digest"
	"github.com/pkg/errors"
)

const whiteoutPrefix = ".wh."

var _ Source = &sourceTar{}

type sourceTar struct {
	file  string
	attrs FileAttrs
	hash  string
}

func NewSourceTar(file string, attrs FileAttrs) Source {
	return &sourceTar{file, attrs, ""}
}

func (s *sourceTar) Type() SourceType {
	return TypeOverlay
}

func (s *sourceTar) Attrs() *FileAttrs {
	return &s.attrs
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
		s.hash = "tar:" + d.String()
	}
	return s.hash, nil
}

func (s *sourceTar) WriteFiles(dest string, w Writer) (err error) {
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

func unpackTar(r io.Reader, dest string, w Writer) error {
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return errors.Wrap(err, "read next tar entry")
		}
		links := map[string]*FileAttrs{}
		if err = unpackTarEntry(hdr, tr, dest, w, links); err != nil {
			return errors.Wrapf(err, "unpack tar entry: %s", hdr.Name)
		}
		for path, a := range links {
			if err = w.Link(path, *a); err != nil {
				return errors.Wrapf(err, "unpack tar entry link: %s", hdr.Name)
			}
		}
	}
	return nil
}

// Derived from umoci's tar_extract.go to allow separate source/dest interfaces
// and filter archive contents on extraction
func unpackTarEntry(hdr *tar.Header, r io.Reader, dest string, w Writer, links map[string]*FileAttrs) (err error) {
	path := layer.CleanPath(filepath.Join(dest, hdr.Name))
	dir, file := filepath.Split(path)

	// Remove file if whiteout
	if strings.HasPrefix(file, whiteoutPrefix) {
		file = strings.TrimPrefix(file, whiteoutPrefix)
		return w.Remove(filepath.Join(dir, file))
	}

	// Convert attributes
	fi := hdr.FileInfo()
	xa := make([]XAttr, 0, len(hdr.Xattrs))
	for k, v := range hdr.Xattrs {
		xa = append(xa, XAttr{k, []byte(v)})
	}
	a := FileAttrs{
		Mode:     fi.Mode(),
		UserIds:  idutils.UserIds{uint(hdr.Uid), uint(hdr.Gid)},
		Xattrs:   xa,
		Link:     hdr.Linkname,
		Atime:    hdr.AccessTime,
		Mtime:    hdr.ModTime,
		Devmajor: hdr.Devmajor,
		Devminor: hdr.Devminor,
	}

	// Create parent directories
	if dir != "." {
		if err = w.DirImplicit(dir, FileAttrs{Mode: 0755}); err != nil {
			return
		}
	}

	// Write file
	switch hdr.Typeflag {
	// regular file
	case tar.TypeReg, tar.TypeRegA:
		delete(links, path)
		err = w.File(path, r, a)
	// directory
	case tar.TypeDir:
		delete(links, path)
		err = w.Dir(path, a)
	// hard link
	case tar.TypeLink:
		a.Link = filepath.Join(string(filepath.Separator)+dest, a.Link)
		links[path] = &a
	// symbolic link
	case tar.TypeSymlink:
		if filepath.IsAbs(a.Link) {
			a.Link = filepath.Join(string(filepath.Separator)+dest, a.Link)
		}
		delete(links, path)
		err = w.Symlink(path, a)
	// character device node, block device node
	case tar.TypeChar, tar.TypeBlock:
		delete(links, path)
		err = w.Block(path, a)
	// fifo node
	case tar.TypeFifo:
		delete(links, path)
		err = w.Fifo(path, a)
	default:
		err = errors.Errorf("unpack entry: %s: unknown typeflag '\\x%x'", hdr.Name, hdr.Typeflag)
	}
	return
}
