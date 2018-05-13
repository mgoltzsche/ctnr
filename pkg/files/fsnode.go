package files

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/mgoltzsche/cntnr/pkg/idutils"
	"github.com/opencontainers/go-digest"
	"github.com/pkg/errors"
)

// TODO: build mtree from the provided file pattern/URLs and
//       use it to generate a cacheable change value and finally as copy input.
//       (Source referenced as xattr? -> maybe use custom tree with each entry holding: Source, destination, metadata)

const (
	lookup lookupWalker = "path lookup"
	mkdirs mkdirsWalker = "mkdirs"
)

var (
	srcDir         = &sourceDir{FileAttrs{Mode: os.ModeDir | 0755, UserIds: idutils.UserIds{uint(os.Geteuid()), uint(os.Getegid())}}}
	srcDirImplicit = &sourceDirImplicit{sourceDir{FileAttrs{Mode: os.ModeDir | 0755, UserIds: idutils.UserIds{uint(os.Geteuid()), uint(os.Getegid())}}}}
)

type FsNode struct {
	name    string
	source  Source
	parent  *FsNode
	child   *FsNode
	next    *FsNode
	overlay bool
}

func NewFS() *FsNode {
	return &FsNode{name: ".", source: srcDirImplicit}
}

func (f *FsNode) Path() (p string) {
	if f.parent == nil {
		return "."
	} else {
		p = filepath.Join(f.parent.Path(), f.name)
	}
	return
}

func (f *FsNode) Hash() (digest.Digest, error) {
	d := digest.SHA256.Digester()
	err := f.WriteTo(d.Hash())
	return d.Digest(), err
}

func (f *FsNode) WriteTo(w io.Writer) (err error) {
	return f.writeTo(w, map[Source]string{})
}

func (f *FsNode) writeTo(w io.Writer, written map[Source]string) (err error) {
	if f.changed() {
		path := f.Path()
		a := f.source.Attrs()
		hash, err := f.source.Hash()
		if err != nil {
			return errors.Wrap(err, "write metadata of "+f.Path())
		}
		link := a.Link
		if f.source.Type() == TypeLink {
			if linkDest, ok := written[f.source]; ok {
				link = filepath.Clean(string(filepath.Separator) + linkDest)
			} else {
				written[f.source] = path
			}
		}
		s := a.Mode.String() + " " + path + " usr=" + a.UserIds.String()
		if a.Size > 0 {
			s += fmt.Sprintf(" size=%d", a.Size)
		}
		if hash != "" {
			s += fmt.Sprintf(" hash=%s", hash)
		}
		if link != "" {
			s += fmt.Sprintf(" link=%q", link)
		}
		if len(a.Xattrs) > 0 {
			al := make([]string, len(a.Xattrs))
			for i, attr := range a.Xattrs {
				al[i] = fmt.Sprintf("%q=%x", attr.Key, attr.Value)
			}
			s += fmt.Sprintf(" xattrs=%s", strings.Join(al, " "))
		}
		if _, err = w.Write([]byte(s + "\n")); err != nil {
			return errors.Wrap(err, "write fstree")
		}
	}
	if f.child != nil {
		if err = f.child.writeTo(w, written); err != nil {
			return
		}
	}
	if f.next != nil {
		err = f.next.writeTo(w, written)
	}
	return
}

func (f *FsNode) changed() bool {
	return f.source != srcDirImplicit || f.child == nil
}

func (f *FsNode) WriteFiles(w Writer) (err error) {
	return f.writeFiles(w, map[Source]string{})
}

func (f *FsNode) writeFiles(w Writer, written map[Source]string) (err error) {
	if f.name != "" || f.source != srcDirImplicit {
		path := f.Path()
		write := true
		if f.source.Type() == TypeLink {
			if linkDest, ok := written[f.source]; ok {
				// link to already written file
				a := *f.source.Attrs()
				a.Link = filepath.Clean(string(filepath.Separator) + linkDest)
				if err = w.Link(path, a); err != nil {
					return
				}
				write = false
			} else {
				// Write file at first hardlink occurrence
				written[f.source] = path
			}
		}
		if write {
			if err = f.source.WriteFiles(path, w); err != nil {
				return
			}
		}
	}
	if f.child != nil {
		err = f.child.writeFiles(w, written)
	}
	if f.next != nil && err == nil {
		err = f.next.writeFiles(w, written)
	}
	return
}

func (f *FsNode) Child(name string) *FsNode {
	if name == "" {
		return nil
	}
	if name == "." {
		return f
	}
	c := f.child
	for c != nil {
		if c.name == name {
			return c
		}
		c = c.next
	}
	return c
}

func (f *FsNode) Add(src Source, dest string) (r *FsNode, err error) {
	if src == nil {
		src = srcDir
	}
	if r, err = f.addNode(dest, src.Type()); err == nil {
		r.source = src
	}
	return
}

func (f *FsNode) addNode(path string, t SourceType) (r *FsNode, err error) {
	path = filepath.Clean(path)
	dir := filepath.Dir(path)
	var p *FsNode
	if dir == "." {
		p = f
	} else {
		if p, err = f.mkdirs(dir); err != nil {
			return
		}
	}
	name := filepath.Base(path)
	r = p.Child(name)
	if r == nil {
		r = p.newEntry(name)
	}
	if r.overlay || t.IsOverlay() {
		r = r.newOverlayEntry()
	}
	if t.IsFile() {
		r.child = nil
	}
	return
}

/* TODO: put node that deletes actual files, create parent nodes that don't create actual nodes
func (f *FsNode) RemoveNode(path string) error {
	f, err := f.Node(path)
	if err != nil {
		return errors.WithMessage(err, "remove")
	}

}*/

/*func (f *FsNode) Link(dest string) (r *FsNode, err error) {
	if f.source.Type() != TypeFile {
		return nil, errors.Errorf("cannot link node %s to %s since it is no file but %s", f.Path(), dest, f.source.Type())
	}
	p := f.Parent()
	if p == nil {
		return nil, errors.Errorf("fsnode link: not supported on root node")
	}
	r, err = p.addNode(dest, TypeFile)
	if err != nil {
		return
	}
	// Assign wrapped source to both nodes or reuse existing hard link wrapper
	r.source = f.source
	if r.source.Type() != TypeLink {
		r.source = NewSourceLink(r.source)
		f.source = r.source
	}
	return
}*/

func (f *FsNode) Root() (r *FsNode) {
	r = f
	for r.parent != nil {
		r = r.parent
	}
	return
}

func (f *FsNode) Parent() *FsNode {
	f = f.parent
	for f != nil && f.name == "" {
		if f.parent != nil {
			f = f.parent
		}
	}
	return f
}

func (f *FsNode) Node(path string) (r *FsNode, err error) {
	return f.walkPath(path, lookup)
}

func (f *FsNode) walkPath(path string, handler pathWalker) (r *FsNode, err error) {
	path = filepath.Clean(path)

	// if abs path delegate to root node
	if filepath.IsAbs(path) {
		return f.Root().walkPath(path[1:], handler)
	}

	// Resolve symlinks recursively
	if f.source.Type().IsSymlink() {
		// Resolve link
		if f, err = f.Parent().walkPath(f.source.Attrs().Link, handler); err != nil {
			return nil, err
		}
	}

	handler.Visit(f)

	if path == "." {
		return f, nil // found
	}

	// Resolve relative path segments recursively
	spos := strings.Index(path, string(filepath.Separator))
	name := path
	if spos != -1 {
		name = path[0:spos]
		path = path[spos+1:]
	} else {
		path = "."
	}
	if name == ".." {
		r = f.Parent()
		if r == nil {
			return nil, errors.Errorf("path outside file system root: /%s", filepath.Join(name, path))
		}
	} else {
		r = f.Child(name)
	}
	if r == nil {
		if r, err = handler.NotFound(f, name); err != nil {
			return
		}
	}
	return r.walkPath(path, handler)
}

type pathWalker interface {
	Visit(*FsNode)
	// Returns an error or creates a new node
	NotFound(parent *FsNode, child string) (*FsNode, error)
}

type mkdirsWalker string

func (_ mkdirsWalker) Visit(f *FsNode) {
	if !f.source.Type().IsDir() {
		f.source = srcDirImplicit
	}
}

func (_ mkdirsWalker) NotFound(p *FsNode, name string) (*FsNode, error) {
	if p.overlay {
		p = p.newOverlayEntry()
	}
	return p.newEntry(name), nil
}

type lookupWalker string

func (_ lookupWalker) Visit(f *FsNode) {}

func (_ lookupWalker) NotFound(p *FsNode, name string) (*FsNode, error) {
	return nil, errors.Errorf("path not found: %s", filepath.Join(p.Path(), name))
}

func (f *FsNode) mkdirs(path string) (r *FsNode, err error) {
	if r, err = f.walkPath(path, mkdirs); err != nil {
		err = errors.WithMessage(err, "mkdirs")
	}
	return
}

func (f *FsNode) newEntry(name string) (r *FsNode) {
	r = &FsNode{
		name:   name,
		parent: f,
		source: srcDirImplicit,
	}

	var last *FsNode
	c := f.child
	for c != nil {
		if c.name > name {
			// Insert before
			r.next = c
			break
		}
		last = c
		c = c.next
	}

	// Append
	if last == nil {
		f.child = r
	} else {
		last.next = r
	}

	return
}

func (f *FsNode) newOverlayEntry() (r *FsNode) {
	if !f.source.Type().IsDir() {
		// Reset parent if it is no directory
		f.source = srcDirImplicit
	}
	c := f.child
	if c != nil && !f.overlay {
		// Move existing children into "" items children to preserve insertion order
		f.child = nil
		f.newEntry("")
		f.child.child = c
	}
	f.overlay = true
	return f.newEntry("")
}
