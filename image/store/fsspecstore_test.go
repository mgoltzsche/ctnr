package store

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"testing"

	"github.com/mgoltzsche/ctnr/image"
	"github.com/mgoltzsche/ctnr/pkg/fs"
	"github.com/mgoltzsche/ctnr/pkg/fs/tree"
	"github.com/opencontainers/go-digest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFsSpecStore(t *testing.T) {
	dir, err := ioutil.TempDir("", ".tmp-test-fsspecstore-")
	require.NoError(t, err)
	defer os.RemoveAll(dir)
	testee := NewFsSpecStore(dir, log.New(os.Stdout, "debug: ", 0))
	fsspec1 := tree.NewFS()
	fsspec2 := tree.NewFS()
	_, err = fsspec2.AddWhiteout("/etc/app.conf")
	require.NoError(t, err)
	for i, fsspec := range []fs.FsNode{fsspec1, fsspec2} {
		id := digest.FromString(fmt.Sprintf("id%d", i))
		putStr := spec2string(t, fsspec)
		err = testee.Put(id, fsspec)
		require.NoError(t, err)

		// Test Get()
		retrieved, err := testee.Get(id)
		require.NoError(t, err)
		retrievedStr := spec2string(t, retrieved)
		assert.Equal(t, putStr, retrievedStr, "retrieved fs spec")

		// Test Exists()
		exists, err := testee.Exists(id)
		require.NoError(t, err)
		assert.True(t, exists, "Exists(existing)")
	}

	// Test Exists(nonExisting)
	exists, err := testee.Exists(digest.FromString("non-existing"))
	require.NoError(t, err, "Exists(nonExisting)")
	assert.True(t, !exists, "!Exists(nonExisting)")

	// Test Get(nonExisting)
	_, err = testee.Get(digest.FromString("non-existing"))
	require.Error(t, err, "Get(nonExisting)")
	assert.True(t, image.IsNotExist(err), "IsNotExist(err)")
}

func spec2string(t *testing.T, spec fs.FsNode) string {
	var buf bytes.Buffer
	err := spec.WriteTo(&buf, fs.AttrsCompare)
	require.NoError(t, err)
	return buf.String()
}
