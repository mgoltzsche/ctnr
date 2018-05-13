package files

import (
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSourceTar(t *testing.T) {
	tmpDir, rootfs := createTestFileSystem(t)
	destfs := filepath.Join(tmpDir, "destfs")
	destPath := filepath.Join("a", "b")
	defer os.RemoveAll(tmpDir)
	tarFile1 := filepath.Join(tmpDir, "a1.tar")
	tarFile2 := filepath.Join(tmpDir, "a2.tar")
	tarDir(t, rootfs, "-cf", tarFile1)
	changedFile := filepath.Join(rootfs, "changedfile")
	err := ioutil.WriteFile(changedFile, []byte("data"), 0644)
	require.NoError(t, err)
	tarDir(t, rootfs, "-cf", tarFile2)

	// Test hash
	testee := newSourceTarTestee(t, tarFile1)
	hash1, err := testee.Hash()
	require.NoError(t, err)
	testee = newSourceTarTestee(t, tarFile2)
	hash2, err := testee.Hash()
	require.NoError(t, err)
	if hash1 == hash2 {
		t.Error("hash1 == hash2")
	}
	testee = newSourceTarTestee(t, tarFile1)
	hash2, err = testee.Hash()
	require.NoError(t, err)
	if hash1 != hash2 {
		t.Error("hash1 != hash1")
	}

	// Test extraction
	wopts := NewFSOptions(true)
	warn := log.New(os.Stdout, "warn: ", 0)
	w := NewDirWriter(destfs, wopts, warn)
	err = w.DirImplicit(filepath.Dir(destPath), FileAttrs{Mode: 0755})
	require.NoError(t, err)
	err = testee.WriteFiles(destPath, w)
	require.NoError(t, err)
	assertFsState(t, filepath.Join(destfs, destPath), "/"+destPath, expectedTestfsState)
}

func tarDir(t *testing.T, dir, opts, tarFile string) {
	cmd := exec.Command("tar", opts, tarFile, ".")
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	require.NoError(t, err)
}

func newSourceTarTestee(t *testing.T, tarFile string) Source {
	testee := NewSourceTar(tarFile, FileAttrs{})

	if testee.Type() != TypeOverlay {
		t.Error("Type() != TypeOverlay")
	}

	if a := testee.Attrs(); a == nil {
		t.Error("Attrs() is nil")
		t.FailNow()
	}

	h, err := testee.Hash()
	require.NoError(t, err)
	if h == "" {
		t.Errorf("%s: hash == ''", tarFile)
		t.FailNow()
	}
	return testee
}
