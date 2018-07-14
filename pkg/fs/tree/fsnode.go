package tree

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/mgoltzsche/cntnr/pkg/fs"
	"github.com/mgoltzsche/cntnr/pkg/fs/source"
	"github.com/opencontainers/go-digest"
	"github.com/pkg/errors"
)

const (
	lookup lookupWalker = "path lookup"
)

var (
	dirAttrs             = fs.FileAttrs{Mode: os.ModeDir | 0755}
	srcOverlayPseudoRoot = sourceNoop("noop source")
	srcParentDir         = sourceParentDir("parent dir source")
	srcWhiteout          = source.NewSourceWhiteout()
)

type sourceParentDir string

func (s sourceParentDir) Attrs() fs.NodeInfo {
	return fs.NodeInfo{fs.TypeDir, fs.FileAttrs{Mode: os.ModeDir | 0755}}
}
func (s sourceParentDir) DeriveAttrs() (fs.DerivedAttrs, error) {
	return fs.DerivedAttrs{}, nil
}
func (s sourceParentDir) Write(dest, name string, w fs.Writer, written map[fs.Source]string) error {
	return w.Mkdir(dest)
}
func (s sourceParentDir) Equal(other fs.Source) (bool, error) {
	return s == other, nil
}

type sourceNoop string

func (s sourceNoop) Attrs() fs.NodeInfo { return fs.NodeInfo{NodeType: fs.TypeDir} }
func (s sourceNoop) DeriveAttrs() (fs.DerivedAttrs, error) {
	return fs.DerivedAttrs{}, nil
}
func (s sourceNoop) Write(dest, name string, w fs.Writer, written map[fs.Source]string) error {
	return nil
}
func (s sourceNoop) Equal(other fs.Source) (bool, error) {
	return s == other, nil
}

type FsNode struct {
	name string
	fs.NodeInfo
	source   fs.Source
	parent   *FsNode
	pathNode *FsNode
	child    *FsNode
	next     *FsNode
	overlay  bool
}

func NewFS() fs.FsNode {
	return newFS()
}

func newFS() *FsNode {
	fs := &FsNode{name: "."}
	fs.pathNode = fs
	fs.SetSource(srcParentDir)
	return fs
}

func (f *FsNode) String() (p string) {
	return f.Path() + " " + f.NodeInfo.AttrString(fs.AttrsAll)
}

func (f *FsNode) Name() string {
	return f.name
}

func (f *FsNode) Path() (p string) {
	if f.parent == nil {
		return string(filepath.Separator)
	} else {
		p = filepath.Join(f.parent.Path(), f.name)
	}
	return
}

func (f *FsNode) Empty() bool {
	return f.source == srcParentDir
}

// Generates the file system's hash including the provided attribute set.
func (f *FsNode) Hash(attrs fs.AttrSet) (digest.Digest, error) {
	d := digest.SHA256.Digester()
	err := f.WriteTo(d.Hash(), attrs)
	return d.Digest(), err
}

// Walks through the tree writing all nodes into the provided Writer.
func (f *FsNode) Write(w fs.Writer) (err error) {
	return f.write(w, map[fs.Source]string{})
}

// Walks through the tree writing all nodes into the provided Writer.
// The 'written' map is used to allow Source implementations to detect and write
// a hardlink.
func (f *FsNode) write(w fs.Writer, written map[fs.Source]string) (err error) {
	if err = f.source.Write(f.Path(), f.name, w, written); err != nil {
		return
	}
	if f.child != nil {
		err = f.child.write(w, written)
	}
	if f.next != nil && err == nil {
		err = f.next.write(w, written)
	}
	return
}

// Returns a file system tree containing only new and changed nodes.
// Nodes that do not exist in the provided file system are returned as whiteouts.
// Hardlinks from new to unchanged (upper to lower) nodes are supported by
// adding hardlinked files to diff as well (to preserve hardlinks and stay
// compatible with external tar tools when writing diff into tar file).
func (f *FsNode) Diff(o fs.FsNode) (r fs.FsNode, err error) {
	node := newFS()
	from := map[string]*FsNode{}
	to := map[string]*FsNode{}
	unchangedSourceMap := map[fs.Source][]string{}
	addedSources := []fs.Source{}
	if err = o.(*FsNode).toMap(to); err == nil {
		err = f.toMap(from)
	}
	if err != nil {
		return nil, errors.WithMessage(err, "diff")
	}
	// Add new or changed nodes to tree
	for k, v := range to {
		old := from[k]
		if old == nil {
			added, err := node.addUpper(k, v.source)
			if err != nil {
				return nil, errors.WithMessage(err, "diff")
			}
			added.parent.applyParents(v.parent)
		} else {
			eq, err := v.Equal(old)
			if err != nil {
				return nil, errors.WithMessage(err, "diff")
			}
			if !eq {
				// changed node - add new source to be written to layer
				added, err := node.addUpper(k, v.source)
				if err != nil {
					return nil, errors.WithMessage(err, "diff")
				}
				added.parent.applyParents(v.parent)
			} else {
				// unchanged node - only map source to support hardlink from upper node
				// Decision regarding hardlink preservation when upper layer contains hardlink to lower layer:
				//  * support hardlinks to lower nodes (pointing to file in different archive) -> does not work with external tar tools, may not work in other engines
				//  * or add hardlinked files to child archive again -> more compatible: works with external tar tools and other storage engines but takes more disk space
				//  => Map source:paths to use add it if linked
				paths := unchangedSourceMap[v.source]
				if paths == nil {
					paths = []string{k}
				} else {
					paths = append(paths, k)
				}
				unchangedSourceMap[v.source] = paths
				continue
			}
		}
		addedSources = append(addedSources, v.source)
	}
	// Add whiteout nodes for those that don't exist in dest tree
	for k := range from {
		p := to[filepath.Dir(k)]
		if to[k] == nil && p != nil {
			added, err := node.AddWhiteoutNode(k)
			if err != nil {
				return nil, errors.WithMessage(err, "diff")
			}
			added.parent.applyParents(p)
		}
	}
	// Add hardlinked nodes (to preserve fs state while remaining compatible with other tools)
	for _, added := range addedSources {
		if paths := unchangedSourceMap[added]; paths != nil {
			for _, path := range paths {
				if _, err = node.AddUpper(path, added); err != nil {
					return nil, errors.WithMessage(err, "diff")
				}
			}
		}
	}
	return node, err
}

func (f *FsNode) Equal(o *FsNode) (bool, error) {
	if !f.source.Attrs().Equal(o.source.Attrs()) {
		return false, nil
	}
	fa, err := f.source.DeriveAttrs()
	if err != nil {
		return false, errors.Wrap(err, "equal")
	}
	oa, err := o.source.DeriveAttrs()
	if err != nil {
		return false, errors.Wrap(err, "equal")
	}
	return fa.Hash == oa.Hash, nil
}

func (f *FsNode) applyParents(o *FsNode) {
	if f != nil && f.source != o.source {
		f.SetSource(o.source)
		f.parent.applyParents(o.parent)
	}
}

func (f *FsNode) toMap(m map[string]*FsNode) (err error) {
	if f.overlay {
		return errors.Errorf("cannot map overlay %s - needs to be normalized first", f.Path())
	}
	m[f.Path()] = f
	if f.child != nil {
		err = f.child.toMap(m)
	}
	if f.next != nil && err == nil {
		err = f.next.toMap(m)
	}
	return
}

func (f *FsNode) findChild(name string) *FsNode {
	if name == "." || name == "/" {
		return f
	}
	if f.overlay {
		return nil
	}
	c := f.child
	for c != nil {
		if c.name == name {
			return c
		}
		if c.name > name {
			return nil
		}
		c = c.next
	}
	return c
}

func (f *FsNode) SetSource(src fs.Source) {
	f.source = src
	f.NodeInfo = src.Attrs()
	if f.NodeType != fs.TypeDir {
		f.child = nil
	}
}

func (f *FsNode) AddUpper(path string, src fs.Source) (r fs.FsNode, err error) {
	return f.addUpper(path, src)
}

func (f *FsNode) addUpper(path string, src fs.Source) (r *FsNode, err error) {
	r, err = f.add(path, src, f.mkdirsUpper)
	return r, errors.Wrap(err, "add upper fsnode")
}

func (f *FsNode) isLowerNode() bool {
	_, ok := f.source.(*fs.NodeAttrs)
	return ok
}

type mkdirsFn func(path string) (r *FsNode, err error)

func (f *FsNode) AddLower(path string, src fs.Source) (r fs.FsNode, err error) {
	r, err = f.add(path, src, f.mkdirsLower)
	return r, errors.Wrap(err, "add lower fsnode")
}
func (f *FsNode) add(path string, src fs.Source, mkdirs mkdirsFn) (r *FsNode, err error) {
	path = filepath.Clean(path)
	dir := filepath.Dir(path)
	var p *FsNode
	if dir == "." {
		p = f
	} else {
		if p, err = mkdirs(dir); err != nil {
			return
		}
	}
	name := filepath.Base(path)
	r = p.findChild(name)
	if r == nil {
		r = p.newEntry(name)
	}
	t := src.Attrs().NodeType
	if t == fs.TypeOverlay {
		r = r.newOverlayEntry()
	} else if r.overlay {
		r = r.overlayRoot()
	} else if t != fs.TypeDir {
		r.child = nil
	}
	r.SetSource(src)
	return
}

func (f *FsNode) AddWhiteout(path string) (fs.FsNode, error) {
	return f.AddWhiteoutNode(path)
}

func (f *FsNode) AddWhiteoutNode(path string) (r *FsNode, err error) {
	if r, err = f.addUpper(path, srcWhiteout); err != nil {
		return nil, errors.WithMessage(err, "add whiteout")
	}
	return
}

// Removes whiteout nodes recursively in all children
func (f *FsNode) RemoveWhiteouts() {
	var (
		c    = f.child
		last *FsNode
	)
	for c != nil {
		if c.NodeType == fs.TypeWhiteout {
			if last == nil {
				f.child = c.next
			} else {
				last.next = c.next
			}
		} else {
			c.RemoveWhiteouts()
			last = c
		}
		c = c.next
	}
}

// Removes this node
func (f *FsNode) Remove() {
	p := f.Parent()
	if p == nil {
		panic("cannot remove detached node") // should not happen
	}
	c := p.child
	var last *FsNode
	for c != nil {
		if c == f {
			if last == nil {
				p.child = c.next
			} else {
				last.next = c.next
			}
			c.parent = nil
			c.next = nil
			c.child = nil
			break
		}
		last = c
		c = c.next
	}
}

func (f *FsNode) Link(path, target string) (linked fs.FsNode, dest fs.FsNode, err error) {
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(path), target)
	}
	targetNode, err := f.node(target)
	if err != nil {
		err = errors.WithMessage(err, "link target")
		return
	}
	linkedNode, err := targetNode.link(path)
	return linkedNode, targetNode, errors.WithMessage(err, "link")
}

func (f *FsNode) link(dest string) (r *FsNode, err error) {
	if f.NodeType == fs.TypeDir || f.NodeType == fs.TypeOverlay {
		return nil, errors.Errorf("cannot link node %s to %s since it is of type %q", f.Path(), dest, f.NodeType)
	}
	p := f.Parent()
	if p == nil {
		return nil, errors.Errorf("link %s: cannot link file system root", dest)
	}
	src := f.source
	if srcLink, ok := src.(*source.SourceUpperLink); ok {
		src = srcLink.Source
	}
	r, err = p.addUpper(dest, source.NewSourceUpperLink(src))
	if err != nil {
		return nil, errors.Wrap(err, "link")
	}
	return
}

func (f *FsNode) Root() (r *FsNode) {
	r = f
	for r.parent != nil {
		r = r.parent
	}
	return
}

func (f *FsNode) Parent() (p *FsNode) {
	return f.pathNode.parent
}

func (f *FsNode) Node(path string) (r fs.FsNode, err error) {
	return f.node(path)
}

func (f *FsNode) node(path string) (r *FsNode, err error) {
	return f.walkPath(path, lookup)
}

func (f *FsNode) walkPath(path string, handler pathWalker) (r *FsNode, err error) {
	path = filepath.Clean(path)

	// if abs path delegate to root node
	if filepath.IsAbs(path) {
		return f.Root().walkPath(path[1:], handler)
	}

	// Resolve symlinks recursively
	if f.NodeType == fs.TypeSymlink {
		// Resolve link
		if f, err = f.Parent().walkPath(f.Symlink, handler); err != nil {
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
		if spos > 0 {
			name = path[0:spos]
		} else {
			name = "."
		}
		path = path[spos+1:]
		if path == "" {
			path = "."
		}
	} else {
		path = "."
	}
	if name == ".." {
		r = f.Parent()
		if r == nil {
			return nil, errors.Errorf("path outside file system root: /%s", filepath.Join(name, path))
		}
	} else {
		r = f.findChild(name)
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

type mkdirsWalker struct {
	overlay bool
}

func (_ *mkdirsWalker) Visit(f *FsNode) {
	if f.NodeType != fs.TypeDir {
		f.SetSource(source.NewSourceDir(dirAttrs))
	}
}

func (w *mkdirsWalker) NotFound(p *FsNode, name string) (r *FsNode, err error) {
	if p.overlay {
		w.overlay = true
		p = p.overlayRoot()
	}
	r = p.newEntry(name)
	if w.overlay {
		r.SetSource(srcParentDir)
	} else {
		r.SetSource(source.NewSourceDir(dirAttrs))
	}
	return
}

type mkdirsUpperWalker struct {
	mkdirsWalker
}

func (_ *mkdirsUpperWalker) Visit(f *FsNode) {
	if f.NodeType != fs.TypeDir {
		f.SetSource(source.NewSourceDir(dirAttrs))
	} else if f.isLowerNode() || f.parent == nil {
		f.SetSource(source.NewSourceDir(f.FileAttrs))
	}
}

type lookupWalker string

func (_ lookupWalker) Visit(f *FsNode) {}

func (_ lookupWalker) NotFound(p *FsNode, name string) (*FsNode, error) {
	return nil, errors.Errorf("path not found: %s", filepath.Join(p.Path(), name))
}

func (f *FsNode) Mkdirs(path string) (r fs.FsNode, err error) {
	return f.mkdirsUpper(path)
}

func (f *FsNode) mkdirsUpper(path string) (r *FsNode, err error) {
	if r, err = f.walkPath(path, &mkdirsUpperWalker{}); err != nil {
		err = errors.WithMessage(err, "Mkdirs")
	}
	return
}

func (f *FsNode) mkdirsLower(path string) (r *FsNode, err error) {
	if r, err = f.walkPath(path, &mkdirsWalker{}); err != nil {
		err = errors.WithMessage(err, "mkdirs")
	}
	return
}

func (f *FsNode) newEntry(name string) (r *FsNode) {
	r = f.newNode(name)
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

func (f *FsNode) newNode(name string) (r *FsNode) {
	r = &FsNode{
		name:   name,
		parent: f.pathNode,
	}
	r.pathNode = r
	return
}

func (f *FsNode) overlayRoot() (r *FsNode) {
	c := f.child
	for c.next != nil {
		c = c.next
	}
	if c.NodeType != fs.TypeOverlay {
		// Return last non-overlay entry (must be . entry)
		return c
	}
	r = f.newNode(".")
	r.SetSource(srcOverlayPseudoRoot)
	r.parent = f.pathNode
	r.pathNode = f
	c.next = r
	return
}

func (f *FsNode) newOverlayEntry() (r *FsNode) {
	if !f.overlay && f.child != nil {
		// Move existing children into overlay item to preserve insertion order
		c := f.child
		f.child = nil
		f.newEntry(".")
		f.child.SetSource(srcOverlayPseudoRoot)
		f.child.pathNode = f
		f.child.child = c
	}
	f.overlay = true
	if f.NodeType != fs.TypeDir {
		f.SetSource(source.NewSourceDir(dirAttrs))
	}
	r = f.newEntry(".")
	r.pathNode = f
	return
}
