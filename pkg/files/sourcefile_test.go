package files

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSourceFile(t *testing.T) {
	tmpDir, err := ioutil.TempDir("", "test-source-dir-")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)
	srcFile := filepath.Join(tmpDir, "sourcefile")
	err = ioutil.WriteFile(srcFile, []byte("content1"), 0644)
	require.NoError(t, err)
	writerMock := newWriterMock(t)
	var mode os.FileMode = 754
	testee := NewSourceFile(srcFile, FileAttrs{Mode: mode})
	if testee.Type() != TypeFile {
		t.Error("Type() != TypeFile")
	}
	a := testee.Attrs()
	if a == nil || a.Mode != mode {
		t.Error("Attrs() incomplete")
		t.FailNow()
	}
	hash1, err := testee.Hash()
	require.NoError(t, err)
	if hash1 == "" {
		t.Error("hash == ''")
		t.FailNow()
	}

	// Test hash
	testee = NewSourceFile(srcFile, FileAttrs{Mode: mode})
	hash2, err := testee.Hash()
	require.NoError(t, err)
	if hash1 != hash2 {
		t.Error("hash1 != hash1")
	}
	err = ioutil.WriteFile(srcFile, []byte("content2"), 0644)
	require.NoError(t, err)
	testee = NewSourceFile(srcFile, FileAttrs{Mode: mode})
	hash2, err = testee.Hash()
	require.NoError(t, err)
	if hash1 == hash2 {
		t.Error("hash1 == hash2")
	}

	// Test write
	err = testee.WriteFiles("/file", writerMock)
	require.NoError(t, err)
	actual := strings.Join(writerMock.written, "\n")
	expected := "- /file"
	if actual != expected {
		t.Errorf("expected %q but was %q", expected, actual)
	}
}
