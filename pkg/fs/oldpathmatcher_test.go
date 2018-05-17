package fs

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGlob(t *testing.T) {
	wd, err := os.Getwd()
	require.NoError(t, err)
	dir, err := ioutil.ReadDir(wd)
	require.NoError(t, err)
	wdl := []string{"."}
	var files []string
	for _, f := range dir {
		files = append(files, f.Name())
	}
	for _, c := range []struct {
		rootfs   string
		pattern  []string
		valid    bool
		expected []string
	}{
		{"", []string{""}, true, wdl},
		{".", []string{""}, true, wdl},
		{"", []string{"./*"}, true, files},
		{"", []string{"./oldpathmatcher_test.go"}, true, []string{"oldpathmatcher_test.go"}},
		{"", []string{filepath.Join(wd, "./*")}, true, files},
		{"", []string{"/*"}, false, nil},
		{"", []string{"../"}, false, nil},
		{wd, []string{""}, true, wdl},
		{wd, []string{"./*"}, true, files},
		{wd, []string{"./oldpathmatcher_test.go"}, true, []string{"oldpathmatcher_test.go"}},
		{wd, []string{filepath.Join(wd, "./*")}, true, files},
		{wd, []string{"/*"}, false, nil},
		{wd, []string{"../"}, false, nil},
		{wd, []string{"./nonexisting"}, false, nil},
		{wd, []string{"\\"}, false, nil},
	} {
		a, err := Glob(c.rootfs, c.pattern)
		if c.valid {
			if err != nil {
				t.Errorf("Glob(%q, %q) returned error: %s", c.rootfs, c.pattern, err)
				continue
			}
			el := []string{}
			for _, e := range c.expected {
				el = append(el, filepath.Join(wd, e))
			}
			sort.Strings(el)
			es := strings.Join(el, "\n")
			as := strings.Join(a, "\n")
			if len(a) != len(c.expected) || es != as {
				t.Errorf("Glob(%q, %q) returned\n\t%+v\nbut expected\n\t%+v", c.rootfs, c.pattern, as, es)
			}
		} else {
			if err == nil {
				t.Errorf("Glob(%q, %q) should return error", c.rootfs, c.pattern)
			}
		}
	}
}

func TestValidateGlob(t *testing.T) {
	for _, c := range []struct {
		pattern []string
		valid   bool
	}{
		{[]string{""}, true},
		{[]string{"root/*"}, true},
		{[]string{"/root/dir/file"}, true},
		{[]string{"/root/dir/*"}, true},
		{[]string{"/*"}, true},
		{[]string{"/**"}, true},
		{[]string{"\\"}, false},
	} {
		if err := ValidateGlob(c.pattern); err == nil && !c.valid || err != nil && c.valid {
			t.Errorf("ValidateGlob(%q) must return %v but returned %s", c.pattern, c.valid, err)
		}
	}
}

func TestWithin(t *testing.T) {
	for _, c := range []struct {
		file  string
		root  string
		valid bool
	}{
		{"/root/dir/file", "/root", true},
		{"/etc/dir/file", "/root", false},
	} {
		if within(c.file, c.root) != c.valid {
			t.Errorf("within(%s, %s) must return %v", c.file, c.root, c.valid)
		}
	}
}
