package store

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"

	digest "github.com/opencontainers/go-digest"
	"github.com/pkg/errors"
	"github.com/vbatts/go-mtree"
)

type LayerSource struct {
	rootfs      string
	rootfsMtree *mtree.DirectoryHierarchy
	paths       []string
	deltafs     bool
}

func (s *BlobStoreOci) NewLayerSource(rootfs string, files []string, deltafs bool) (src *LayerSource, err error) {
	if rootfs == "" {
		return nil, errors.New("rootfs not set")
	}
	rootfsMtree, err := s.mtree.Create(rootfs)
	return &LayerSource{rootfs, rootfsMtree, files, deltafs}, err
}

func (s *LayerSource) DiffHash() (d digest.Digest, err error) {
	var buf bytes.Buffer
	filter := map[string]bool{}
	for _, path := range s.paths {
		addPath(filepath.Clean("/"+path), filter)
	}
	for _, entry := range s.rootfsMtree.Entries {
		path, e := entry.Path()
		if err != nil {
			return d, errors.New("diff hash: " + e.Error())
		}
		if path != "." && path != ".." && path != "/" && path[0] != '/' && (s.paths == nil || filter[filepath.Clean("/"+path)]) {
			keys := strings.Join(mtree.KeyValToString(withoutTime(entry.AllKeys())), " ")
			buf.WriteString(path + "  " + keys + "\n")
		}
	}
	return digest.FromBytes(buf.Bytes()), nil
}

func (s *LayerSource) Close() (err error) {
	if s.deltafs {
		err = os.RemoveAll(s.rootfs)
	}
	return
}

func withoutTime(keys []mtree.KeyVal) []mtree.KeyVal {
	r := make([]mtree.KeyVal, 0, len(keys))
	for _, key := range keys {
		if key.Keyword() != "tar_time" {
			r = append(r, key)
		}
	}
	return r
}

func addPath(path string, paths map[string]bool) {
	if paths[path] {
		return
	}
	paths[path] = true
	addPath(filepath.Dir(path), paths)
}
