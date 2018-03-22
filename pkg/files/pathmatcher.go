package files

import (
	"path/filepath"
	"strings"

	"github.com/pkg/errors"
)

// Returns all files within rootDir that match any provided pattern
func glob(rootDir string, pattern []string) (files []string, err error) {
	for _, p := range pattern {
		if _, err = filepath.Match(p, "/"); err != nil {
			return
		}
	}
	for _, p := range pattern {
		if !filepath.IsAbs(p) {
			p = filepath.Join(rootDir, p)
		}
		if !within(filepath.Clean("/"+p), rootDir) {
			return files, errors.Errorf("file pattern %q is outside context directory", p)
		}
		f, e := filepath.Glob(p)
		if e != nil {
			return files, e
		}
		if len(f) == 0 {
			return files, errors.Errorf("file pattern %q did not match", p)
		}
		files = append(files, f...)
	}
	return
}

// true if file is within rootDir
func within(file, rootDir string) bool {
	a := string(filepath.Separator)
	return strings.Index(file+a, rootDir+a) == 0
}
