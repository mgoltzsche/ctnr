package model

import (
	"encoding/base64"
	"fmt"
	specs "github.com/opencontainers/runtime-spec/specs-go"
	"os"
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

func (self *volumeResolver) PrepareVolumeMount(m VolumeMount) (specs.Mount, error) {
	t := m.Type
	if t == "" {
		t = "bind"
	}
	var src string
	var err error
	if m.IsNamedVolume() {
		src, err = self.named(m)
	} else if m.Source == "" {
		src, err = self.anonymous(m.Target)
	} else {
		src, err = self.path(m.Source)
	}
	opts := m.Options
	if len(opts) == 0 {
		// Apply default mount options. See man7.org/linux/man-pages/man8/mount.8.html
		opts = []string{"rbind", "nodev", "mode=0755"}
	}
	r := specs.Mount{
		Type:        t,
		Destination: m.Target,
		Source:      src,
		Options:     opts,
	}
	if err != nil {
		return r, err
	}

	return r, err
}

func (self *volumeResolver) named(m VolumeMount) (string, error) {
	r, ok := self.volumes[m.Source]
	if !ok {
		return "", fmt.Errorf("Volume %q not found", m.Source)
	}
	if r.Source == "" {
		return self.anonymous("!" + m.Source)
	}
	return r.Source, nil
}

func (self *volumeResolver) anonymous(id string) (f string, err error) {
	id = filepath.Clean(id)
	f = filepath.Join(self.bundleDir, "volumes", base64.RawStdEncoding.EncodeToString([]byte(id)))
	err = os.MkdirAll(f, 0755)
	if err != nil {
		err = fmt.Errorf("Cannot create anonymous volume for %s at %s: %s", id, f, err)
	}
	return f, err
}

func (self *volumeResolver) path(file string) (r string, err error) {
	if !filepath.IsAbs(file) && !(file == "~" || len(file) > 1 && file[0:2] == "~/") {
		file = filepath.Join(self.baseDir, file)
	}

	if _, err = os.Stat(file); os.IsNotExist(err) {
		err = os.MkdirAll(file, 0755)
	}

	return file, err
}
