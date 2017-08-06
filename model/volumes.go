package model

import (
	"encoding/base64"
	"fmt"
	"path/filepath"
)

type volumeResolver struct {
	baseDir   string
	bundleDir string
	volumes   map[string]Volume
}

func NewVolumeResolver(project *Project, bundleDir string) *volumeResolver {
	return &volumeResolver{project.Dir, bundleDir, project.Volumes}
}

func (self *volumeResolver) Named(name string) (string, error) {
	r, ok := self.volumes[name]
	if !ok {
		return "", fmt.Errorf("Volume %q not found", name)
	}
	if r.Source == "" {
		return self.Anonymous("!" + name), nil
	}
	return r.Source, nil
}

func (self *volumeResolver) Anonymous(dest string) string {
	return filepath.Join(self.bundleDir, "volumes", base64.RawStdEncoding.EncodeToString([]byte(dest)))
}

func (self *volumeResolver) Path(path string) string {
	if !filepath.IsAbs(path) && !(path == "~" || len(path) > 1 && path[0:2] == "~/") {
		path = filepath.Join(self.baseDir, path)
	}
	return path
}
