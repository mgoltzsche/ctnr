package writer

import (
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"testing"

	"github.com/mgoltzsche/ctnr/pkg/fs"
	"github.com/mgoltzsche/ctnr/pkg/fs/testutils"
	"github.com/stretchr/testify/require"
)

func TestDirWriter(t *testing.T) {
	tmpDir, err := ioutil.TempDir("", "ctnr-test-dirfswriter-")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)
	opts := fs.NewFSOptions(true)
	warn := log.New(os.Stdout, "warn: ", 0)
	rootfs := filepath.Join(tmpDir, "rootfs")

	testee := NewDirWriter(rootfs, opts, warn)
	testutils.WriteTestFileSystem(t, testee)
	err = testee.Close()
	require.NoError(t, err)

	testutils.AssertFsState(t, rootfs, "", testutils.MtreeTestkeywordsWithTarTime, testutils.ExpectedTestfsState)

	// Test fs boundaries
	fileOutsideRootfs := filepath.Join(tmpDir, "outsiderootfs")
	err = ioutil.WriteFile(fileOutsideRootfs, []byte{}, 0644)
	require.NoError(t, err)
	err = testee.Link("etc/link-invalid", fileOutsideRootfs)
	if err == nil {
		t.Errorf("should return error when linking outside rootfs boundaries")
		t.FailNow()
	}

	invalidSymlinkFile := filepath.Join(rootfs, "etc", "symlink-invalid")
	err = os.Symlink(filepath.Join("..", ".."), invalidSymlinkFile)
	require.NoError(t, err)
	_, err = testee.File("etc/symlink-invalid/filefrominvalidsymlink", testutils.NewSourceMock(fs.TypeFile, fs.FileAttrs{Mode: 0644, Size: 11}, ""))
	if err != nil {
		t.Errorf("should not return error when attempting write linked outside rootfs boundaries (/etc/symlink-invalid/filefrominvalidsymlink) but was: %s", err)
		t.FailNow()
	}
	_, err = os.Stat(filepath.Join(rootfs, "filefrominvalidsymlink"))
	require.NoError(t, err, "invalid symlink parent resolution")
	err = os.Remove(invalidSymlinkFile)
	require.NoError(t, err)

	// Test testee reuses existing dir
	testee = NewDirWriter(rootfs, opts, warn)
	err = testee.Dir(".", "", fs.FileAttrs{Mode: os.ModeDir | 0755})
	require.NoError(t, err)
}
