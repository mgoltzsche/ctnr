package source

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mgoltzsche/ctnr/pkg/fs"
	"github.com/mgoltzsche/ctnr/pkg/idutils"
	"github.com/stretchr/testify/require"
)

func TestSourceFifo(t *testing.T) {
	mtime, err := time.Parse(time.RFC3339, "2018-01-23T01:01:42Z")
	require.NoError(t, err)
	mtime = time.Unix(mtime.Unix(), 900000000)
	atime, err := time.Parse(time.RFC3339, "2018-01-23T01:02:42Z")
	require.NoError(t, err)
	tmpDir, err := ioutil.TempDir("", "test-source-dir-")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)
	srcFile := filepath.Join(tmpDir, "sourcefile")
	err = ioutil.WriteFile(srcFile, []byte("content1"), 0644)
	require.NoError(t, err)
	const mode os.FileMode = 0664
	usr := idutils.UserIds{1, 33}
	fileAttrs := fs.FileAttrs{Mode: mode, UserIds: usr, FileTimes: fs.FileTimes{Atime: atime, Mtime: mtime}}
	testee := NewSourceFifo(fs.DeviceAttrs{fileAttrs, 0, 0})
	a := testee.Attrs()
	if a.NodeType != fs.TypeFifo {
		t.Error("type != TypeFifo")
		t.FailNow()
	}
	if a.Mode != mode {
		t.Errorf("mode %s != %s", a.Mode, mode)
	}

	// Test write
	assertSourceWriteWithHardlinkSupport(t, testee, "/file type=fifo usr=1:33 mode=664 mtime=1516669302 atime=1516669362")
}
