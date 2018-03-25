package store

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/openSUSE/umoci/pkg/fseval"
	"github.com/vbatts/go-mtree"
)

func TestDiffHash(t *testing.T) {
	rootfs, file := mockRootfs()
	defer os.RemoveAll(rootfs)
	testee := testeeFromRootfs(rootfs)
	hash1 := testee.DiffHash().String()
	time.Sleep(time.Duration(1100000000))
	testee = testeeFromRootfs(rootfs)
	hash2 := testee.DiffHash().String()
	if hash1 != hash2 {
		t.Errorf("Same content must result in the same hash")
	}
	if err := ioutil.WriteFile(file, []byte("content"), 0640); err != nil {
		panic(err)
	}
	testee = testeeFromRootfs(rootfs)
	hash2 = testee.DiffHash().String()
	if hash1 == hash2 {
		t.Errorf("Different content should result in different hash")
	}
}

func mockRootfs() (rootfs, file string) {
	rootfs, err := ioutil.TempDir("", "cntnr-test-fsdiff-")
	if err != nil {
		panic(err)
	}
	dir := filepath.Join(rootfs, "dir")
	if err = os.Mkdir(dir, 0755); err != nil {
		panic(err)
	}
	f, err := ioutil.TempFile(dir, "file")
	if err != nil {
		panic(err)
	}
	file = f.Name()
	f.Close()
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
