package files

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pkg/errors"
)

// Returns all files within rootDir that match any provided glob pattern.
// To treat dest as parent directory it must end with the path separator character.
func Glob(rootDir string, pattern []string) (files []string, err error) {
	if !filepath.IsAbs(rootDir) {
		wd, e := os.Getwd()
		if e != nil {
			return nil, errors.New(e.Error())
		}
		rootDir = filepath.Join(wd, rootDir)
	}
	if err = ValidateGlob(pattern); err != nil {
		return
	}
	rootDir = filepath.Clean(rootDir)
	for _, p := range pattern {
		p = filepath.Join(rootDir, p)
		if !within(p, rootDir) {
			return files, errors.Errorf("file pattern %q is outside context directory", p)
		}
		f, e := filepath.Glob(p)
		if e != nil {
			return files, errors.New(e.Error())
		}
		if len(f) == 0 {
			return files, errors.Errorf("file pattern %q did not match", p)
		}
		files = append(files, f...)
	}
	sort.Strings(files)
	return
}

func ValidateGlob(pattern []string) (err error) {
	for _, p := range pattern {
		if _, err = filepath.Match(p, string(filepath.Separator)); err != nil {
			return errors.New(err.Error())
		}
	}
	return
}

// true if file is within rootDir
func within(file, rootDir string) bool {
	a := string(filepath.Separator)
	return strings.Index(file+a, rootDir+a) == 0
}
