package tree

import (
	"io"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/mgoltzsche/cntnr/pkg/fs"
	"github.com/mgoltzsche/cntnr/pkg/fs/writer"
	"github.com/pkg/errors"
)

func ParseFsSpec(b []byte) (r fs.FsNode, err error) {
	r = NewFS()
	current := r
	pathMap := map[string]*fs.NodeAttrs{}
	for i, line := range strings.Split(string(b), "\n") {
		line := strings.TrimSpace(line)
		sp := strings.Index(line, " ")
		if sp == -1 {
			// Handle '..'
			if current, err = current.Node(line); err != nil {
				return nil, errors.Wrapf(err, "parse fs spec: line %d", i)
			}
		} else {
			// Add entry
			name, err := url.PathUnescape(line[:sp])
			if err != nil {
				return nil, errors.Wrapf(err, "parse fs spec: line %d", i)
			}
			attrStr := strings.TrimSpace(line[sp:])
			if strings.HasPrefix(attrStr, "hlink=") {
				// hardlink
				linkDest := filepath.Clean(attrStr[6:])
				linkSrc := pathMap[linkDest]
				path := filepath.Join(current.Path(), name)
				if linkSrc == nil {
					return nil, errors.Errorf("parse fs spec: line %d: link %s destination %q does not exist", i, path, linkDest)
				}
				if _, err = current.AddLower(name, linkSrc); err != nil {
					return nil, errors.Wrapf(err, "parse fs spec: line %d", i)
				}
			} else if attrStr == "type="+string(fs.TypeDir) {
				// implicit parent dir
				if current, err = current.AddLower(name, srcParentDir); err != nil {
					return nil, errors.Wrapf(err, "parse fs spec: line %d", i)
				}
				current.SetSource(srcParentDir)
			} else {
				// any other node
				attrs, err := fs.ParseNodeAttrs(attrStr)
				if err != nil {
					return nil, errors.Wrapf(err, "parse fs spec: line %d", i)
				}
				var newNode fs.FsNode
				newNode, err = current.AddLower(name, &attrs)
				if err != nil {
					return nil, errors.Wrapf(err, "parse fs spec: line %d", i)
				}
				src := srcWhiteout
				if attrs.NodeType != fs.TypeWhiteout {
					src = &attrs
					pathMap[newNode.Path()] = &attrs
				}
				newNode.SetSource(src)
				if src.Attrs().NodeType == fs.TypeDir {
					// TODO: maybe remove ugly type assertion
					current = newNode.(*FsNode).pathNode
				}
			}
		}
	}
	return
}

func (f *FsNode) WriteTo(w io.Writer, attrs fs.AttrSet) (err error) {
	return f.writeTo(w, attrs, map[fs.Source]string{})
}

func (f *FsNode) writeTo(w io.Writer, attrs fs.AttrSet, written map[fs.Source]string) (err error) {
	sw := writer.NewStringWriter(w, attrs)
	if err = f.writeSpecLine(sw, attrs, written); err != nil {
		return
	}
	if f.child != nil {
		if err = f.child.genFileSpecLines(sw, attrs, written); err != nil {
			return
		}
		if err = f.child.genDirSpecLines(sw, attrs, written); err != nil {
			return
		}
	}
	return
}

func (f *FsNode) genFileSpecLines(w fs.Writer, attrs fs.AttrSet, written map[fs.Source]string) (err error) {
	if f.NodeType != fs.TypeDir && f.NodeType != fs.TypeOverlay {
		err = f.writeSpecLine(w, attrs, written)
	}
	if f.next != nil && err == nil {
		err = f.next.genFileSpecLines(w, attrs, written)
	}
	return
}

func (f *FsNode) genDirSpecLines(w fs.Writer, attrs fs.AttrSet, written map[fs.Source]string) (err error) {
	if f.NodeType == fs.TypeDir || f.NodeType == fs.TypeOverlay {
		if err = f.writeSpecLine(w, attrs, written); err != nil {
			return
		}
		if f.child != nil {
			if err = f.child.genFileSpecLines(w, attrs, written); err != nil {
				return
			}
			if err = f.child.genDirSpecLines(w, attrs, written); err != nil {
				return
			}
		}
		// Generate '..' line
		if f.name != "." {
			if err = w.Parent(); err != nil {
				return
			}
		}
	}
	if f.next != nil {
		err = f.next.genDirSpecLines(w, attrs, written)
	}
	return
}

func (f *FsNode) writeSpecLine(w fs.Writer, attrs fs.AttrSet, written map[fs.Source]string) error {
	return f.source.Write(f.Path(), f.name, w, written)
}
