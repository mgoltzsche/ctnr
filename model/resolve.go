package model

import (
	"encoding/base32"
	"path/filepath"

	"github.com/pkg/errors"
)

type PathResolver interface {
	ResolveFile(string) string
}

type pathResolver string

func NewPathResolver(baseDir string) PathResolver {
	return pathResolver(baseDir)
}

func (self pathResolver) ResolveFile(file string) string {
	baseDir := string(self)
	file = filepath.Clean(file)
	if !filepath.IsAbs(file) && !(file == "~" || len(file) > 1 && file[0:2] == "~/") {
		file = filepath.Join(baseDir, file)
	}
	return file
}

type ResourceResolver interface {
	PathResolver
	ResolveMountSource(VolumeMount) (string, error)
}

type resourceResolver struct {
	PathResolver
	volumes map[string]Volume
}

func NewResourceResolver(paths PathResolver, vols map[string]Volume) ResourceResolver {
	return &resourceResolver{paths, vols}
}

func (self *resourceResolver) ResolveMountSource(m VolumeMount) (src string, err error) {
	if m.Source == "" || m.Type == MOUNT_TYPE_TMPFS {
		src = self.anonymous(m.Target)
	} else if m.Type == MOUNT_TYPE_VOLUME {
		src, err = self.named(m.Source)
	} else {
		src = self.path(m.Source)
	}
	return
}

func (self *resourceResolver) named(src string) (string, error) {
	var (
		v  Volume
		ok bool
	)
	if self.volumes != nil {
		v, ok = self.volumes[src]
	}
	if !ok {
		return "", errors.Errorf("volume %q not found", src)
	}
	if v.Source == "" {
		return self.anonymous("!" + src), nil
	}
	return v.Source, nil
}

func (self *resourceResolver) anonymous(id string) string {
	id = filepath.Clean(id)
	return filepath.Join("volumes", base32.StdEncoding.EncodeToString([]byte(id)))
}

func (self *resourceResolver) path(file string) string {
	return self.ResolveFile(file)
}
