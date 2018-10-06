package store

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mgoltzsche/cntnr/image"
	"github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBlobStore(t *testing.T) {
	dir, err := ioutil.TempDir("", ".tmp-test-kvstore-")
	require.NoError(t, err)
	defer os.RemoveAll(dir)
	testee := NewBlobStore(dir)
	for i, s := range []string{"content1", "content2"} {
		key := digest.FromString(fmt.Sprintf("id%d", i))
		written, err := testee.Put(key, strings.NewReader(s))
		require.NoError(t, err)
		assert.Equal(t, int64(len(s)), written, "written bytes")

		// Test Get()
		reader, err := testee.Get(key)
		require.NoError(t, err)
		defer func() {
			err = reader.Close()
			require.NoError(t, err)
		}()
		b, err := ioutil.ReadAll(reader)
		require.NoError(t, err)
		assert.Equal(t, s, string(b), "retrieved content for key %s", key)

		// Test Exists()
		exists, err := testee.Exists(key)
		require.NoError(t, err)
		assert.True(t, exists, "Exists(existing)")
	}

	// Test Keys()
	key1 := digest.FromString("id0")
	key2 := digest.FromString("id1")
	key3 := digest.FromString("id3")
	_, err = testee.Put(key3, strings.NewReader("asdf"))
	require.NoError(t, err)
	err = ioutil.WriteFile(filepath.Join(dir, "danglingdir"), []byte("x"), 0644)
	require.NoError(t, err)
	err = ioutil.WriteFile(filepath.Join(dir, "sha256", "danglingfile"), []byte("x"), 0644)
	require.NoError(t, err)
	err = os.Mkdir(filepath.Join(dir, "sha256", "danglingdir"), 0755)
	require.NoError(t, err)
	assertKeys := func(keys []digest.Digest) {
		keys, err := testee.Keys()
		require.NoError(t, err, "Keys()")
		for i, key := range keys {
			assert.Contains(t, keys, key, "Keys() should contain key%d", i)
		}
	}
	assertKeys([]digest.Digest{key1, key2, key3})

	// Test Delete()
	keyDel := digest.FromString("keydel")
	_, err = testee.Put(keyDel, strings.NewReader("asdf"))
	require.NoError(t, err)
	err = testee.Delete(keyDel)
	require.NoError(t, err, "Delete()")
	assertKeys([]digest.Digest{key1, key2, key3})

	// Test Retain()
	err = testee.Retain(map[digest.Digest]bool{key1: true, key3: true})
	require.NoError(t, err, "Retain()")
	assertKeys([]digest.Digest{key1, key3})

	// Test Exists(nonExisting)
	nonExistingKey := digest.FromString("non-existing")
	exists, err := testee.Exists(nonExistingKey)
	require.NoError(t, err, "Exists(nonExisting)")
	assert.True(t, !exists, "!Exists(nonExisting)")

	// Test Get(nonExisting)
	_, err = testee.Get(nonExistingKey)
	require.Error(t, err, "Get(nonExisting)")
	assert.True(t, image.IsNotExist(err), "IsNotExist(err)")
}
