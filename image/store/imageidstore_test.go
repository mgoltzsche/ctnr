package store

import (
	"fmt"
	"io/ioutil"
	"os"
	"testing"

	"github.com/mgoltzsche/cntnr/image"
	"github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestImageIdStore(t *testing.T) {
	dir, err := ioutil.TempDir("", ".tmp-test-imageidstore-")
	require.NoError(t, err)
	defer os.RemoveAll(dir)
	testee := NewImageIdStore(dir)
	for i, _ := range []int{1, 2} {
		id := digest.FromString(fmt.Sprintf("id%d", i))
		mdigest := digest.FromString(fmt.Sprintf("manifest%d", i))
		// Test Put()
		err = testee.Put(id, mdigest)
		require.NoError(t, err)

		// Test Get()
		retrieved, err := testee.Get(id)
		require.NoError(t, err)
		assert.Equal(t, ImageID{id, mdigest}, retrieved, "retrieved fs spec")
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
