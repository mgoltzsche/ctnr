package fs

import (
	"net/url"
	"testing"
	"time"

	"github.com/mgoltzsche/ctnr/pkg/idutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFileAttrsEquals(t *testing.T) {
	mtime, err := time.Parse(time.RFC3339, "2018-01-23T01:01:42Z")
	require.NoError(t, err)
	atime, err := time.Parse(time.RFC3339, "2018-01-23T01:02:42Z")
	require.NoError(t, err)
	otime, err := time.Parse(time.RFC3339, "2018-01-23T01:03:42Z")
	require.NoError(t, err)
	a := FileAttrs{
		Mode:      0644,
		UserIds:   idutils.UserIds{0, 33},
		Xattrs:    map[string]string{"k": "v"},
		FileTimes: FileTimes{mtime, atime},
		Size:      7,
		Symlink:   "../symlinkdest",
	}
	assert.True(t, a.Equal(&a), "a.Equal(a)")
	b := a
	assert.True(t, a.Equal(&b), "a.Equal(b)")
	b.Xattrs = nil
	c := b
	assert.True(t, b.Equal(&c), "nilXAttrs.Equal(nilXAttrs)")
	b.Xattrs = map[string]string{}
	c = b
	assert.True(t, b.Equal(&c), "emptyXAttrs.Equal(emptyXAttrs)")
	c.Xattrs = nil
	assert.True(t, b.Equal(&c), "emptyXAttrs.Equal(nilXAttrs)")
	b = a
	b.Mode = 0640
	assert.False(t, a.Equal(&b), "a.Equal(changedMode)")
	b = a
	b.Uid = 1
	assert.False(t, a.Equal(&b), "a.Equal(changedUid)")
	b = a
	b.Gid = 1000
	assert.False(t, a.Equal(&b), "a.Equal(changedUid)")
	b = a
	b.Xattrs = map[string]string{"k": "v", "x": "y"}
	assert.False(t, a.Equal(&b), "a.Equal(changedXAttrsSize)")
	b.Xattrs = map[string]string{"k": "x"}
	assert.False(t, a.Equal(&b), "a.Equal(changedXAttr)")
	b.Xattrs = nil
	assert.False(t, a.Equal(&b), "a.Equal(nilXAttrs)")
	b = a
	b.Size = 3
	assert.False(t, a.Equal(&b), "a.Equal(changedSize)")
	b = a
	b.Symlink = "changeddest"
	assert.False(t, a.Equal(&b), "a.Equal(changedSymlink)")
	b = a
	b.Mtime = otime
	assert.False(t, a.Equal(&b), "a.Equal(changedMtime)")
	b = a
	b.Atime = otime
	assert.True(t, a.Equal(&b), "a.Equal(changedAtime)")
}

func TestNodeInfoEquals(t *testing.T) {
	a := NodeInfo{TypeFile, FileAttrs{Mode: 0644}}
	assert.True(t, a.Equal(a), "a.Equal(a)")
	b := a
	b.NodeType = TypeSymlink
	assert.False(t, a.Equal(b), "a.Equal(changedNodeType)")
	b = a
	b.Mode = 0600
	assert.False(t, a.Equal(b), "a.Equal(changedMode)")
}

func TestDerivedAttrsEqual(t *testing.T) {
	a := DerivedAttrs{"hash", "url", "httpinfo"}
	assert.True(t, a.Equal(&a), "a.Equal(a)")
	b := a
	a.Hash = "changed"
	assert.False(t, a.Equal(&b), "a.Equal(changedHash)")
	b = a
	a.URL = "changed"
	assert.False(t, a.Equal(&b), "a.Equal(changedURL)")
	b = a
	a.HTTPInfo = "changed"
	assert.False(t, a.Equal(&b), "a.Equal(changedHTTPInfo)")
	b = a
}

func TestNodeAttrsString(t *testing.T) {
	mtime, err := time.Parse(time.RFC3339, "2018-01-23T01:01:42Z")
	require.NoError(t, err)
	atime, err := time.Parse(time.RFC3339, "2018-02-23T01:02:42Z")
	require.NoError(t, err)
	parsed, err := url.Parse("http://example.org/my page")
	require.NoError(t, err)
	da := DerivedAttrs{"sha256:hex", parsed.String(), "http= info"}
	testee := NodeAttrs{
		NodeInfo{
			TypeFile,
			FileAttrs{
				Mode:    0750,
				UserIds: idutils.UserIds{33, 99},
				Xattrs:  map[string]string{"k1= x": "v1= x", "k2": "v2"},
				Symlink: "link= dest/x",
				Size:    123,
				FileTimes: FileTimes{
					Atime: atime,
					Mtime: mtime,
				},
			},
		},
		da,
	}
	actual := testee.AttrString(AttrsAll)
	expected := "type=file usr=33:99 mode=750 size=123 link=link=%20dest/x xattr.k1%3D+x=v1%3D+x xattr.k2=v2 mtime=1516669302 atime=1519347762 hash=sha256:hex url=http://example.org/my%20page http=http%3D+info"
	if expected != actual {
		t.Errorf("attrs.AttrString(): expected\n  %s\nbut was\n  %s", expected, actual)
		t.FailNow()
	}
	a, err := ParseNodeAttrs(expected)
	require.NoError(t, err)
	actual = a.AttrString(AttrsAll)
	if expected != actual {
		t.Errorf("ParseAttrs(a).String() != a: expected\n  %s\nbut was\n  %s", expected, actual)
	}
}
