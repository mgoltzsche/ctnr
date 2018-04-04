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
	delta       bool
}

func (s *LayerSource) DiffHash() digest.Digest {
	var buf bytes.Buffer
	_, err := s.rootfsMtree.WriteTo(&buf)
	if err != nil {
		panic(err)
	}

	lines := strings.Split(buf.String(), "\n")
	normalizedLines := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.Trim(line, " ")
		if line != "" && line[0] != '#' {
			normalizedLines = append(normalizedLines, line)
		}
	}
	return digest.FromString(strings.Join(normalizedLines, "\n"))
}

func (s *BlobStoreOci) NewLayerSource(rootfs string, filterPaths []string) (src *LayerSource, err error) {
	if rootfs == "" {
		return nil, errors.New("rootfs not set")
	}
	delta := filterPaths != nil
	var excludes []mtree.ExcludeFunc
	if delta {
		filterMap := map[string]bool{}
		for _, file := range filterPaths {
			addPath(filepath.Join(rootfs, file), filterMap)
		}
		excludes = append(excludes, func(path string, info os.FileInfo) bool {
			return !filterMap[path]
		})
	}
	rootfsMtree, err := s.mtree.Create(rootfs, excludes)
	return &LayerSource{rootfs, rootfsMtree, delta}, err
}

func addPath(path string, paths map[string]bool) {
	if paths[path] {
		return
	}
	paths[path] = true
	addPath(filepath.Dir(path), paths)
}
