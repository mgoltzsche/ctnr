package source

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mgoltzsche/cntnr/pkg/fs"
	"github.com/mgoltzsche/cntnr/pkg/idutils"
	"github.com/openSUSE/umoci/pkg/fseval"
	"github.com/stretchr/testify/require"
)

func TestSourceFile(t *testing.T) {
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
	fsEval := fseval.RootlessFsEval
	const mode os.FileMode = 0754
	fileAttrs := fs.FileAttrs{Mode: mode, FileTimes: fs.FileTimes{Atime: atime, Mtime: mtime}}
	testee := NewSourceFile(fs.NewFileReader(srcFile, fsEval), fileAttrs)
	a := testee.Attrs()
	if a.NodeType != fs.TypeFile {
		t.Error("type != TypeFile")
		t.FailNow()
	}
	if a.Mode != mode {
		t.Errorf("DerivedAttrs() mode %s != %s", a.Mode, mode)
	}
	wa, err := testee.DeriveAttrs()
	require.NoError(t, err)
	if wa.Hash == "" {
		t.Errorf("DerivedAttrs() hash == ''")
	}

	// Test hash
	hash1 := wa.Hash
	testee = NewSourceFile(fs.NewFileReader(srcFile, fsEval), fileAttrs)
	wa, err = testee.DeriveAttrs()
	require.NoError(t, err)
	hash2 := wa.Hash
	if hash1 != hash2 {
		t.Error("hash1 != hash1")
	}
	err = ioutil.WriteFile(srcFile, []byte("content2"), 0644)
	require.NoError(t, err)
	fileAttrs.UserIds = idutils.UserIds{1, 33}
	testee = NewSourceFile(fs.NewFileReader(srcFile, fsEval), fileAttrs)
	wa, err = testee.DeriveAttrs()
	require.NoError(t, err)
	hash2 = wa.Hash
	if hash1 == hash2 {
		t.Error("hash1 == hash2")
	}

	// Test write
	assertSourceWriteWithHardlinkSupport(t, testee, "/file type=file usr=1:33 mode=754 mtime=1516669302 atime=1516669362 hash="+hash2)
}
