package store

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/openSUSE/umoci/pkg/fseval"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vbatts/go-mtree"
)

func TestDiffHash(t *testing.T) {
	files := []string{"/dir/sdir/f1", "/dir/sdir/f2", "/x"}
	rootfs := mockRootfs(files)
	defer os.RemoveAll(rootfs)

	// Assert hash reproducable
	testee := testeeFromRootfs(rootfs)
	hash1, err := testee.DiffHash(nil)
	require.NoError(t, err)
	time.Sleep(time.Duration(500000000))
	testee = testeeFromRootfs(rootfs)
	hash2, err := testee.DiffHash(nil)
	require.NoError(t, err)
	assert.Equal(t, hash1, hash2, "Same content must result in the same hash")

	// Assert changed content -> different hash
	err = ioutil.WriteFile(filepath.Join(rootfs, "dir", "file"), []byte("content"), 0640)
	require.NoError(t, err)
	testee = testeeFromRootfs(rootfs)
	hash2, err = testee.DiffHash(nil)
	require.NoError(t, err)
	assert.NotEqual(t, hash1, hash2, "Different content should result in different hash")

	// Assert filtered source should result in same hash
	testee = testeeFromRootfs(rootfs)
	hash2, err = testee.DiffHash([]string{"/dir/sdir/f1", "/dir/sdir/f2", "/x"})
	require.NoError(t, err)
	assert.Equal(t, hash1, hash2, "Filtered content must result in same hash")
}

func mockRootfs(files []string) (rootfs string) {
	var err error
	if rootfs, err = ioutil.TempDir("", "cntnr-test-fsdiff-"); err != nil {
		panic(err)
	}
	for _, file := range files {
		file = filepath.Join(rootfs, file)
		if err = os.MkdirAll(filepath.Dir(file), 0755); err == nil {
			if err = ioutil.WriteFile(file, []byte("asdf"), 0644); err == nil {
				continue
			}
		}
		panic(err)
	}
	return
}

func testeeFromRootfs(rootfs string) *LayerSource {
	dh, err := mtree.Walk(rootfs, nil, mtreeKeywords, fseval.RootlessFsEval)
	if err != nil {
		panic(err)
	}
	return &LayerSource{
		rootfs:      rootfs,
		rootfsMtree: dh,
	}
}
