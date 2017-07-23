package images

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// See https://github.com/opencontainers/image-tools/blob/master/image/manifest.go#unpackLayer
// TODO: Replace with image-tools when image tools includes runtime-spec v1.0.0 since current older dependency causes conflict
func unpackLayer(src, dest string) error {
	fr, err := os.OpenFile(src, os.O_RDONLY, 0444)
	if err != nil {
		return err
	}
	defer fr.Close()
	gr, err := gzip.NewReader(fr)
	if err != nil {
		return err
	}
	defer gr.Close()
	tr := tar.NewReader(gr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		hdr.Name = filepath.Clean(hdr.Name)
		path := filepath.Join(dest, hdr.Name)

		rel, err := filepath.Rel(dest, path)
		if err != nil {
			return err
		}
		info := hdr.FileInfo()
		if strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			return fmt.Errorf("%q is outside of image directory %q", hdr.Name, dest)
		}

		// Remove file that has been removed in current layer
		if strings.HasPrefix(info.Name(), ".wh.") {
			path = strings.Replace(path, ".wh.", "", 1)

			if err := os.RemoveAll(path); err != nil {
				return fmt.Errorf("Unable to delete whiteout path %s: %v", path, err)
			}

			continue
		}

		// Create parent directory
		if !strings.HasSuffix(hdr.Name, string(os.PathSeparator)) {
			// Not the root directory, ensure that the parent directory exists
			parent := filepath.Dir(hdr.Name)
			parentPath := filepath.Join(dest, parent)
			if _, err2 := os.Lstat(parentPath); err2 != nil && os.IsNotExist(err2) {
				if err3 := os.MkdirAll(parentPath, 0755); err3 != nil {
					return err3
				}
			}
		}

		// Extract entry
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(path, os.FileMode(hdr.Mode)); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			info := hdr.FileInfo()

			// TODO: remove if link only
			err = removeIfExists(path)
			if err != nil {
				return err
			}

			f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, info.Mode())
			if err != nil {
				return fmt.Errorf("Unable open file: %v", err)
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return fmt.Errorf("Unable to copy: %v", err)
			}
			f.Close()
		case tar.TypeLink:
			target := filepath.Join(dest, hdr.Linkname)

			if !strings.HasPrefix(target, dest) {
				return fmt.Errorf("invalid hardlink in layer: %q -> %q", target, hdr.Linkname)
			}

			// TODO: remove if no link only
			err = removeIfExists(path)
			if err != nil {
				return err
			}

			if err = os.Link(target, path); err != nil {
				return err
			}

		case tar.TypeSymlink:
			target := filepath.Join(filepath.Dir(path), hdr.Linkname)

			if !strings.HasPrefix(target, dest) {
				return fmt.Errorf("invalid symlink in layer: %q -> %q", path, hdr.Linkname)
			}

			// TODO: remove if no link only
			err = removeIfExists(path)
			if err != nil {
				return err
			}

			if err = os.Symlink(hdr.Linkname, path); err != nil {
				return err
			}
		case tar.TypeXGlobalHeader:
			return nil
		default:
			// TODO: eventually return error
			os.Stderr.WriteString(fmt.Sprintf("Unsupported entry type: %c - %s\n", hdr.Typeflag, hdr.Name))
		}
	}
	return nil
}

func removeIfExists(f string) error {
	if _, err := os.Stat(f); !os.IsNotExist(err) {
		return os.Remove(f)
	}
	return nil
}

func extractFile(dest string, reader *tar.Reader) error {
	f, err := os.OpenFile(dest, os.O_RDWR|os.O_TRUNC, 0770)
	if err != nil {
		f, err = os.Create(dest)
		if err != nil {
			return err
		}
	}
	defer f.Close()
	_, err = io.Copy(f, reader)
	return err
}
