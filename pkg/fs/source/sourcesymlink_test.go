package source

import (
	"testing"
	"time"

	"github.com/mgoltzsche/cntnr/pkg/fs"
	"github.com/mgoltzsche/cntnr/pkg/idutils"
	"github.com/stretchr/testify/require"
)

func TestSourceSymlink(t *testing.T) {
	mtime, err := time.Parse(time.RFC3339, "2018-01-23T01:01:42Z")
	require.NoError(t, err)
	mtime = time.Unix(mtime.Unix(), 900000000)
	atime, err := time.Parse(time.RFC3339, "2018-01-23T01:02:42Z")
	require.NoError(t, err)
	testee := sourceSymlink{fs.FileAttrs{Symlink: "../symlinkdest", UserIds: idutils.UserIds{1, 33}, FileTimes: fs.FileTimes{Atime: atime, Mtime: mtime}}}
	a := testee.Attrs()
	if a.NodeType != fs.TypeSymlink {
		t.Error("type != TypeSymlink")
		t.FailNow()
	}
	if a.Symlink == "" {
		t.Error("symlink does not provide destination path")
		t.FailNow()
	}

	// Test write
	assertSourceWriteWithHardlinkSupport(t, &testee, "/file type=symlink usr=1:33 link=../symlinkdest mtime=1516669302 atime=1516669362")

	// Test Equal()
	eq, err := (&testee).Equal(&testee)
	require.NoError(t, err)
	require.True(t, eq, "Equal(): should equal same instance")
	other := testee
	eq, err = (&testee).Equal(&other)
	require.NoError(t, err)
	require.True(t, eq, "Equal(): should equal its copy")
	other.attrs.Symlink = "changed"
	eq, err = (&testee).Equal(&other)
	require.NoError(t, err)
	require.False(t, eq, "Equal(): should not equal when changed")
}
