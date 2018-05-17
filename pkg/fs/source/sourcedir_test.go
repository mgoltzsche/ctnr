package source

import (
	"io/ioutil"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mgoltzsche/cntnr/pkg/fs"
	"github.com/mgoltzsche/cntnr/pkg/fs/testutils"
	"github.com/mgoltzsche/cntnr/pkg/idutils"
	"github.com/stretchr/testify/require"
)

func TestSourceDir(t *testing.T) {
	mtime, err := time.Parse(time.RFC3339, "2018-01-23T01:01:42Z")
	require.NoError(t, err)
	mtime = time.Unix(mtime.Unix(), 900000000)
	atime, err := time.Parse(time.RFC3339, "2018-01-23T01:02:42Z")
	require.NoError(t, err)
	tmpDir, err := ioutil.TempDir("", "test-source-dir-")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)
	writerMock := testutils.NewWriterMock(t, fs.AttrsAll)
	var mode os.FileMode = 0750
	times := fs.FileTimes{Atime: atime, Mtime: mtime}
	usr := idutils.UserIds{1, 33}
	testee := NewSourceDir(fs.FileAttrs{Mode: mode, UserIds: usr, FileTimes: times})
	a := testee.Attrs()
	if a.NodeType != fs.TypeDir {
		t.Error("Attrs(): type != TypeDir")
		t.FailNow()
	}
	if a.Mode != mode {
		t.Errorf("Attrs(): mode %s != %s", a.Mode, mode)
	}

	testee.Write("/dir", "", writerMock, nil)
	actual := strings.Join(writerMock.Written, "\n")
	expected := "/dir type=dir usr=1:33 mode=750 mtime=1516669302 atime=1516669362"
	if actual != expected {
		t.Errorf("expected\n  %q\nbut was\n  %q", expected, actual)
	}

	// Test Equal()
	eq, err := testee.Equal(testee)
	require.NoError(t, err)
	require.True(t, eq, "Equal(): should equal same instance")
	other := NewSourceDir(fs.FileAttrs{Mode: mode, UserIds: usr, FileTimes: times})
	eq, err = testee.Equal(other)
	require.NoError(t, err)
	require.True(t, eq, "Equal(): should equal when copy provided")
	other = NewSourceDir(fs.FileAttrs{Mode: 444, FileTimes: times})
	eq, err = testee.Equal(other)
	require.NoError(t, err)
	require.False(t, eq, "Equal(): should not equal when changed")
	eq, err = testee.Equal(NewSourceFile(fs.NewReadableBytes([]byte("content")), fs.FileAttrs{Mode: 0444}))
	require.NoError(t, err)
	require.False(t, eq, "Equal(): should not equal when no dir source provided")
}
