package store

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/opencontainers/go-digest"
	ispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
)

func TestLockedImageRepo(t *testing.T) {
	tmpDir, err := ioutil.TempDir("", ".tmp-test-lockedimagerepo-")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)
	dir := filepath.Join(tmpDir, "repodir")
	extBlobDir := filepath.Join(dir, "repodir")
	err = os.MkdirAll(extBlobDir, 0755)
	require.NoError(t, err)

	testee, err := NewLockedImageRepo("testrepo", dir, extBlobDir)
	require.NoError(t, err, "NewLockedImageRepo()")

	// Test Manifests()
	dl, err := testee.Manifests()
	require.NoError(t, err, "Manifests()")
	require.Equal(t, 0, len(dl), "len(Manifests())")

	// Test AddManifest()
	newManifest1 := newDescriptor(digest.FromString("new"), "new")
	testee.AddManifest(newManifest1)
	dl, err = testee.Manifests()
	require.NoError(t, err, "Manifests()")
	require.Equal(t, []ispecs.Descriptor{newManifest1}, dl, "Manifests() after insertion before flush")
	err = testee.Close()
	require.NoError(t, err, "add and flush")
	// Reopen to check persistence
	testee, err = NewLockedImageRepo("testrepo", dir, extBlobDir)
	require.NoError(t, err, "open existing")
	dl, err = testee.Manifests()
	require.NoError(t, err, "Manifests()")
	require.Equal(t, []ispecs.Descriptor{newManifest1}, dl, "Manifests() after insertion and flush")

	// Test DeleteManifest(existing)
	newManifest2 := newDescriptor(digest.FromString("new"), "2bDeleted")
	newManifest3 := newDescriptor(digest.FromString("newer"), "newer")
	testee.AddManifest(newManifest2)
	testee.AddManifest(newManifest3)
	err = testee.Close()
	require.NoError(t, err)
	testee, err = NewLockedImageRepo("testrepo", dir, extBlobDir)
	require.NoError(t, err)
	testee.DelManifest("2bDeleted")
	err = testee.Close()
	require.NoError(t, err, "delete and flush")
	testee, err = NewLockedImageRepo("testrepo", dir, extBlobDir)
	require.NoError(t, err, "open existing after deletion")
	dl, err = testee.Manifests()
	require.NoError(t, err)
	require.Equal(t, []ispecs.Descriptor{newManifest3, newManifest1}, dl, "Manifests() after deletion and flush")

	// Test DeleteManifest(notExisting)
	testee.DelManifest("not-existing")
	err = testee.Close()
	require.Error(t, err, "delete not existing")

	// Test Retain()
	testee, err = NewLockedImageRepo("testrepo", dir, extBlobDir)
	require.NoError(t, err)
	testee.AddManifest(newManifest2)
	testee.Retain(map[digest.Digest]bool{newManifest3.Digest: true})
	err = testee.Close()
	require.NoError(t, err, "retain and flush")
	testee, err = NewLockedImageRepo("testrepo", dir, extBlobDir)
	require.NoError(t, err)
	dl, err = testee.Manifests()
	require.NoError(t, err)
	require.Equal(t, []ispecs.Descriptor{newManifest3}, dl, "Manifests() after retain and flush")
	testee.Retain(map[digest.Digest]bool{})
	err = testee.Close()
	require.NoError(t, err)

	// Test Limit()
	testee, err = NewLockedImageRepo("testrepo-limited", dir, extBlobDir)
	require.NoError(t, err)
	manifests := make([]ispecs.Descriptor, 10)
	for i := 0; i < 10; i++ {
		ref := fmt.Sprintf("ref%d", i)
		manifests[i] = newDescriptor(digest.FromString(ref), ref)
		testee.AddManifest(manifests[i])
	}
	testee.Limit(5)
	dl, err = testee.Manifests()
	require.NoError(t, err)
	manifests = manifests[5:]
	for i := 0; i < len(manifests)/2; i++ { // revert
		tmp := manifests[i]
		manifests[i] = manifests[4-i]
		manifests[4-i] = tmp
	}
	require.Equal(t, manifests, dl, "Manifests() after limit")
	testee.Retain(map[digest.Digest]bool{})
	err = testee.Close()
	require.NoError(t, err)

	// Test repo is clear after all deleted
	fl, err := ioutil.ReadDir(tmpDir)
	require.NoError(t, err)
	require.Equal(t, []os.FileInfo{}, fl, "parent dir should be empty after all repo manifests have been deleted")
}
