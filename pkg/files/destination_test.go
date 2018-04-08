package files

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMap(t *testing.T) {
	for _, c := range []struct {
		src      []string
		dest     string
		expected []CopyPair
	}{
		{[]string{"/a"}, "/d", []CopyPair{
			CopyPair{"/a", "/d"},
		}},
		{[]string{"/a"}, "/d/", []CopyPair{
			CopyPair{"/a", "/d/a"},
		}},
		{[]string{"/a", "/dir/b"}, "/d", []CopyPair{
			CopyPair{"/a", "/d/a"},
			CopyPair{"/dir/b", "/d/b"},
		}},
		{[]string{"/a", "/dir/b"}, "/d/", []CopyPair{
			CopyPair{"/a", "/d/a"},
			CopyPair{"/dir/b", "/d/b"},
		}},
	} {
		a := Map(c.src, c.dest)
		assert.Equal(t, srcDestString(c.expected), srcDestString(a), "pairs("+strings.Join(c.src, " ")+", "+c.dest+")")
	}
}

func srcDestString(p []CopyPair) string {
	r := make([]string, len(p))
	for i, e := range p {
		r[i] = fmt.Sprintf("%-7s %-7s", e.Source, e.Dest)
	}
	return strings.Join(r, "\n")
}

/*func TestHash(t *testing.T) {
	ctxDir := mockRootfs(t, []string{"/data/d1", "data/d2", "/etc/srv1/conf", "/etc/srv2/conf", "/dirx/file1", "/file"})
	d, err := Hash([]CopyPair{
		CopyPair{filepath.Join(ctxDir, "/data"), "/d"},
		CopyPair{filepath.Join(ctxDir, "/etc"), "/etc/srv"},
		CopyPair{filepath.Join(ctxDir, "/dirx/file1"), "/file1"},
	}, true)
	require.NoError(t, err)
	assert.True(t, string(d) != "", "Hash() should not be empty")
}*/

func mockRootfs(t *testing.T, files []string) (rootfs string) {
	var err error
	rootfs, err = ioutil.TempDir("", "cntnr-test-hash-")
	require.NoError(t, err)
	for _, file := range files {
		file = filepath.Join(rootfs, file)
		err = os.MkdirAll(filepath.Dir(file), 0755)
		require.NoError(t, err)
		err = ioutil.WriteFile(file, []byte("asdf"), 0644)
		require.NoError(t, err)
	}
	return
}
