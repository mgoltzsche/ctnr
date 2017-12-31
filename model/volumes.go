package model

import (
	"encoding/base64"
	"fmt"
	"path/filepath"

	specs "github.com/opencontainers/runtime-spec/specs-go"
)

type volumeResolver struct {
	baseDir string
	volumes map[string]Volume
}

func NewVolumeResolver(project *Project) *volumeResolver {
	return &volumeResolver{project.Dir, project.Volumes}
}

func (self *volumeResolver) PrepareVolumeMount(m VolumeMount) (r specs.Mount, err error) {
	t := m.Type
	if t == "" {
		t = "bind"
	}
	var src string
	if m.IsNamedVolume() {
		src, err = self.named(m)
	} else if m.Source == "" {
		src = self.anonymous(m.Target)
	} else {
		src = self.path(m.Source)
	}

	opts := m.Options
	if len(opts) == 0 {
		// Apply default mount options. See man7.org/linux/man-pages/man8/mount.8.html
		opts = []string{"bind", "nodev", "mode=0755"}
	} else {
		foundBindOpt := false
		for _, opt := range opts {
			if opt == "bind" || opt == "rbind" {
				foundBindOpt = true
				break
			}
		}
		if !foundBindOpt {
			opts = append(opts, "bind")
		}
	}

	r = specs.Mount{
		Type:        t,
		Source:      src,
		Destination: m.Target,
		Options:     opts,
	}
	return
}

func (self *volumeResolver) named(m VolumeMount) (string, error) {
	r, ok := self.volumes[m.Source]
	if !ok {
		return "", fmt.Errorf("Volume %q not found", m.Source)
	}
	if r.Source == "" {
		return self.anonymous("!" + m.Source), nil
	}
	return r.Source, nil
}

func (self *volumeResolver) anonymous(id string) string {
	id = filepath.Clean(id)
	return filepath.Join("volumes", base64.RawStdEncoding.EncodeToString([]byte(id)))
}

func (self *volumeResolver) path(file string) string {
	return resolveFile(file, self.baseDir)
}

func resolveFile(file, baseDir string) string {
	file = filepath.Clean(file)
	if !filepath.IsAbs(file) && !(file == "~" || len(file) > 1 && file[0:2] == "~/") {
		file = filepath.Join(baseDir, file)
	}
	return file
}
