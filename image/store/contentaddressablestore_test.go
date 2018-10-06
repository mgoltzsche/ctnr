package store

import (
	"io/ioutil"
	"os"
	"strings"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"
)

func TestContentAddressableStore(t *testing.T) {
	dir, err := ioutil.TempDir("", ".tmp-test-contentaddressablestore-")
	require.NoError(t, err)
	defer os.RemoveAll(dir)
	testee := NewContentAddressableStore(dir)
	var lastDigest digest.Digest
	for _, s := range []string{"content1", "content2"} {
		// Test put
		key1, written, err := testee.Put(strings.NewReader(s))
		require.NoError(t, err)
		require.NoError(t, key1.Validate(), "Put().Validate()")
		require.Equal(t, int64(len(s)), written, "written bytes")
		key2, written, err := testee.Put(strings.NewReader(s))
		require.NoError(t, err)
		require.NoError(t, key2.Validate(), "Put().Validate()")
		require.Equal(t, int64(len(s)), written, "written bytes")
		require.Equal(t, key1, key2, "KEY(Put(a)) == KEY(Put(a))")
		require.NotEqual(t, key1, lastDigest, "KEY(Put(a)) != KEY(Put(b))")
		lastDigest = key1

		// Test Get()
		reader, err := testee.Get(key1)
		require.NoError(t, err)
		defer func() {
			err = reader.Close()
			require.NoError(t, err)
		}()
		b, err := ioutil.ReadAll(reader)
		require.NoError(t, err)
		require.Equal(t, s, string(b), "retrieved content for key %s", key1)

		// Test Exists()
		exists, err := testee.Exists(key1)
		require.NoError(t, err)
		require.True(t, exists, "Exists(existing)")
	}
}
