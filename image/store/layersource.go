package store

import (
	"bytes"
	"path/filepath"
	"strings"

	digest "github.com/opencontainers/go-digest"
	"github.com/pkg/errors"
	"github.com/vbatts/go-mtree"
)

type LayerSource struct {
	rootfs      string
	rootfsMtree *mtree.DirectoryHierarchy
	addOnly     bool
}

func (s *LayerSource) DiffHash(paths []string) (d digest.Digest, err error) {
	var buf bytes.Buffer
	filter := map[string]bool{}
	for _, path := range paths {
		addPath(filepath.Clean("/"+path), filter)
	}
	for _, entry := range s.rootfsMtree.Entries {
		path, e := entry.Path()
		if err != nil {
			return d, errors.New("is delta: " + e.Error())
		}
		if path != "." && path != ".." && path != "/" && path[0] != '/' && (paths == nil || filter[filepath.Clean("/"+path)]) {
			keys := strings.Join(mtree.KeyValToString(withoutTime(entry.AllKeys())), " ")
			buf.WriteString(path + "  " + keys + "\n")
		}
	}
	return digest.FromBytes(buf.Bytes()), nil
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

func (s *BlobStoreOci) NewLayerSource(rootfs string, addOnly bool) (src *LayerSource, err error) {
	if rootfs == "" {
		return nil, errors.New("rootfs not set")
	}
	rootfsMtree, err := s.mtree.Create(rootfs)
	return &LayerSource{rootfs, rootfsMtree, addOnly}, err
}

func addPath(path string, paths map[string]bool) {
	if paths[path] {
		return
	}
	paths[path] = true
	addPath(filepath.Dir(path), paths)
}
