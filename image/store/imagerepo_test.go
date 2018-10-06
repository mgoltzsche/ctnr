package store

import (
	"encoding/json"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/opencontainers/go-digest"
	ispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
)

func TestImageRepo(t *testing.T) {
	dir, err := ioutil.TempDir("", ".tmp-test-imagerepo-")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	testee, err := NewImageRepo("testrepo", dir)
	require.Error(t, err, "NewImageRepo(nonexisting)")
	testee, expectedManifests := newTestImageRepo(t, dir)

	// Test Manifests()
	dl, err := testee.Manifests()
	require.NoError(t, err, "Manifests()")
	require.Equal(t, expectedManifests, dl, "Manifests()")

	// Test Manifest()
	expectedManifest := expectedManifests[1]
	d, err := testee.Manifest(expectedManifest.Annotations[ispecs.AnnotationRefName])
	require.NoError(t, err, "Manifest(expected.ref)")
	require.Equal(t, expectedManifest, d, "Manifest(expected.ref)")

	_, err = testee.Manifest("non-existing")
	require.Error(t, err, "Manifest(non-existing)")
}

func newTestImageRepo(t *testing.T, dir string) (repo *ImageRepo, manifests []ispecs.Descriptor) {
	manifestDigest1 := digest.FromString("1")
	manifests = []ispecs.Descriptor{
		newDescriptor(digest.FromString("2"), "2"),
		newDescriptor(manifestDigest1, "3"),
		newDescriptor(manifestDigest1, "latest"),
	}
	mockIdx := ispecs.Index{Manifests: manifests}
	mockIdx.SchemaVersion = 2
	b, err := json.Marshal(&mockIdx)
	require.NoError(t, err)
	err = ioutil.WriteFile(filepath.Join(dir, "index.json"), b, 0644)
	require.NoError(t, err)

	repo, err = NewImageRepo("testrepo", dir)
	require.NoError(t, err)
	return
}

func newDescriptor(d digest.Digest, ref string) ispecs.Descriptor {
	return ispecs.Descriptor{
		MediaType: ispecs.MediaTypeImageManifest,
		Digest:    d,
		Size:      123456,
		Platform: &ispecs.Platform{
			Architecture: runtime.GOARCH,
			OS:           runtime.GOOS,
		},
		Annotations: map[string]string{
			ispecs.AnnotationRefName: ref,
		},
	}
}
