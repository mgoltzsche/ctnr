package fs

import (
	"io"

	"github.com/mgoltzsche/cntnr/pkg/idutils"
	"github.com/openSUSE/umoci/pkg/fseval"
	"github.com/opencontainers/go-digest"
)

// TODO: also support opaque whiteout (.wh..wh..opq): https://github.com/opencontainers/image-spec/blob/master/layer.md#opaque-whiteout
const WhiteoutPrefix = ".wh."

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

type FsNode interface {
	Name() string
	Path() string
	Empty() bool
	SetSource(src Source)
	Node(path string) (FsNode, error)
	Mkdirs(path string) (FsNode, error)
	Link(path, targetPath string) (FsNode, FsNode, error)
	AddUpper(path string, src Source) (FsNode, error)
	AddLower(path string, src Source) (FsNode, error)
	AddWhiteout(path string) (FsNode, error)
	Remove()
	WriteTo(w io.Writer, attrs AttrSet) error
	Write(Writer) error
	Diff(FsNode) (FsNode, error)
	Hash(AttrSet) (digest.Digest, error)
}
