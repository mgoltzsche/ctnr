package writer

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/mgoltzsche/cntnr/pkg/fs"
	"github.com/pkg/errors"
)

var _ fs.Writer = &TarWriter{}

// A mapping file system writer that secures root directory boundaries.
// Derived from umoci's tar_extract.go to allow separate source/dest interfaces
// and filter archive contents on extraction
type TarWriter struct {
	writer  *tar.Writer
	written map[string]*fs.FileAttrs
}

func NewTarWriter(writer io.Writer) (w *TarWriter) {
	return &TarWriter{tar.NewWriter(writer), map[string]*fs.FileAttrs{}}
}

type TarGzWriter struct {
	TarWriter
	gzWriter *gzip.Writer
}

/*func NewTarGzWriter(writer io.Writer) (w *TarGzWriter) {
	// TODO: compress in different goroutine as done in umoci's layer.GenerateLayer()
	// Actually do not compress here but in store since store/image builder must know plain tar's hash as diffId
	gzWriter := gzip.NewWriter(writer)
	return &TarGzWriter{TarWriter{tar.NewWriter(gzWriter), map[string]*FileAttrs{}}, gzWriter}
}

func (w *TarGzWriter) Close() error {
	e1 := w.TarWriter.Close()
	e2 := w.gzWriter.Close()
	if e1 == nil {
		return e2
	} else {
		return e1
	}
}*/

func (w *TarWriter) Close() error {
	return w.writer.Close()
}

func (w *TarWriter) Parent() error                                        { return nil }
func (w *TarWriter) LowerNode(path, name string, a *fs.NodeAttrs) error   { return nil }
func (w *TarWriter) LowerLink(path, target string, a *fs.NodeAttrs) error { return nil }

func (w *TarWriter) Lazy(path, name string, src fs.LazySource, written map[fs.Source]string) (err error) {
	return errors.Errorf("refused to write lazy source %s into tar writer directly at %s since resulting tar could contain overridden entries. lazy sources must be resolved first.", src, path)
}

func (w *TarWriter) File(path string, src fs.FileSource) (r fs.Source, err error) {
	a := src.Attrs()
	if path, err = normalize(path); err != nil {
		return
	}
	if err = w.writeTarHeader(path, a.FileAttrs); err != nil {
		return
	}

	if a.NodeType != fs.TypeFile {
		return src, nil
	}

	// Copy file
	f, err := src.Reader()
	if err != nil {
		return
	}
	defer func() {
		if e := f.Close(); e != nil && err == nil {
			err = errors.Wrap(e, "write tar")
		}
	}()
	n, err := io.Copy(w.writer, f)
	if err != nil {
		return nil, errors.Wrap(err, "write tar: file entry")
	}
	if n != a.Size {
		return nil, errors.Wrap(io.ErrShortWrite, "write tar: file entry")
	}
	return src, nil
}

func (w *TarWriter) writeTarHeader(path string, a fs.FileAttrs) (err error) {
	hdr, err := w.toTarHeader(path, a)
	if err != nil {
		return
	}
	err = w.writer.WriteHeader(hdr)
	return errors.Wrap(err, "tar writer")
}

func (w *TarWriter) toTarHeader(path string, a fs.FileAttrs) (hdr *tar.Header, err error) {
	a.Mtime = time.Unix(a.Mtime.Unix(), 0) // use floor(mtime) to preserve mtime which otherwise is not guaranteed due to rounding to seconds within tar
	hdr, err = tar.FileInfoHeader(fs.NewFileInfo(path, &a), a.Symlink)
	if err != nil {
		return nil, errors.Wrapf(err, "to tar header: %s", path)
	}
	hdr.AccessTime = a.Atime
	hdr.Xattrs = a.Xattrs
	w.addWritten(path, &a)
	return
}

// Taken from umoci
func normalize(path string) (string, error) {
	path = filepath.Clean(string(os.PathSeparator) + path)
	path, _ = filepath.Rel(string(os.PathSeparator), path)
	path = filepath.Clean(path)
	if !isValidPath(path) {
		return "", errors.Errorf("tar writer: path outside tar root: %s", path)
	}
	return path, nil
}

func isValidPath(path string) bool {
	prfx := string(os.PathSeparator) + "___"
	return filepath.HasPrefix(filepath.Join(prfx, path), prfx)
}

func (w *TarWriter) addWritten(path string, a *fs.FileAttrs) {
	w.written[path] = a
}

func (w *TarWriter) Link(path, target string) (err error) {
	if path, err = normalize(path); err != nil {
		return
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(path), target)
	}
	if target, err = normalize(target); err != nil {
		return errors.Wrap(err, "link")
	}

	a := w.written[target]
	if a == nil {
		return errors.Errorf("write tar: link entry %s: target %s does not exist", path, target)
	}
	hdr, err := w.toTarHeader(path, *a)
	if err != nil {
		return
	}
	hdr.Typeflag = tar.TypeLink
	hdr.Linkname = target
	hdr.Size = 0

	err = w.writer.WriteHeader(hdr)
	return errors.Wrap(err, "tar writer: write link")
}

func (w *TarWriter) Symlink(path string, a fs.FileAttrs) (err error) {
	if path, err = normalize(path); err != nil {
		return
	}

	a.Mode |= os.ModeSymlink | 0777
	a.Symlink, err = normalizeLinkDest(path, a.Symlink)
	if err != nil {
		return
	}
	return w.writeTarHeader(path, a)
}

func (w *TarWriter) Fifo(path string, a fs.DeviceAttrs) (err error) {
	a.Mode |= syscall.S_IFIFO
	return w.device(path, &a)
}

func (w *TarWriter) device(path string, a *fs.DeviceAttrs) (err error) {
	if path, err = normalize(path); err != nil {
		return
	}
	hdr, err := w.toTarHeader(path, a.FileAttrs)
	if err != nil {
		return
	}
	hdr.Size = 0
	hdr.Devmajor = a.Devmajor
	hdr.Devminor = a.Devminor
	err = w.writer.WriteHeader(hdr)
	return errors.Wrap(err, "tar writer: write device")
}

func (w *TarWriter) Device(path string, a fs.DeviceAttrs) (err error) {
	return w.device(path, &a)
}

func (w *TarWriter) Mkdir(path string) (err error) {
	if path, err = normalize(path); err != nil {
		return
	}
	return w.writeTarHeader(path+string(os.PathSeparator), fs.FileAttrs{Mode: os.ModeDir | 0755})
}

func (w *TarWriter) Dir(path, base string, a fs.FileAttrs) (err error) {
	if path, err = normalize(path); err != nil {
		return
	}
	return w.writeTarHeader(path+string(os.PathSeparator), a)
}

func (w *TarWriter) Remove(path string) (err error) {
	if path, err = normalize(path); err != nil {
		return
	}
	delete(w.written, path)
	dir, file := filepath.Split(path)
	file = fs.WhiteoutPrefix + file
	now := time.Now()
	// Using current time for header values which leads to unreproducable layer
	// TODO: maybe change to fixed time instead of now()
	return w.writeTarHeader(filepath.Join(dir, file), fs.FileAttrs{FileTimes: fs.FileTimes{Atime: now, Mtime: now}})
}
