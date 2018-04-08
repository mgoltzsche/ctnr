package store

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/mgoltzsche/cntnr/pkg/log"
	"github.com/openSUSE/umoci/pkg/fseval"
	digest "github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vbatts/go-mtree"
)

func TestMtreeStore(t *testing.T) {
	tmpDir, err := ioutil.TempDir("", ".mtree-store-test")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)
	rootfs := filepath.Join(tmpDir, "rootfs")
	err = os.Mkdir(rootfs, 0755)
	require.NoError(t, err)
	testee := NewMtreeStore(tmpDir, fseval.RootlessFsEval, log.NewNopLogger())
	_, err = testee.Get(digest.FromString("asdf"))
	assert.True(t, IsMtreeNotExist(err), "_, err = Get(nonexisting); IsNotExist(err)")
	dhl := []*mtree.DirectoryHierarchy{}
	// Assert different contents result in different mtrees
	for i, _ := range []int{1, 2} {
		file := fmt.Sprintf("file%d", i)
		err = ioutil.WriteFile(filepath.Join(rootfs, file), []byte("content1"), 0644)
		require.NoError(t, err)
		dh, err := testee.Create(rootfs)
		require.NoError(t, err)
		// Assert one mtree can be stored/retrieved using multiple keys
		for j, key := range []digest.Digest{digest.FromString(file + "key2"), digest.FromString(file + "key1")} {
			err = testee.Put(key, dh)
			require.NoError(t, err)
			a, err := testee.Get(key)
			require.NoError(t, err)
			delta, err := testee.Diff(dh, a)
			require.NoError(t, err)
			var buf bytes.Buffer
			dh.WriteTo(normWriter(&buf))
			assert.True(t, len(delta) == 0, fmt.Sprintf("Put(key%d, dh); Get(key%d) == dh\nWAS: %v\nEXPECTED: %s", j, j, delta, buf.String()))
		}
		dhl = append(dhl, dh)
	}
	delta, err := testee.Diff(dhl[0], dhl[1])
	require.NoError(t, err)
	assert.False(t, len(delta) == 0, "mtree1 == mtree2")
}
