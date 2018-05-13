package files

import (
	"io/ioutil"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSourceDir(t *testing.T) {
	tmpDir, err := ioutil.TempDir("", "test-source-dir-")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)
	writerMock := newWriterMock(t)
	var mode os.FileMode = 0750
	testee := NewSourceDir(FileAttrs{Mode: mode})
	if testee.Type() != TypeDir {
		t.Error("Type() != TypeDir")
	}
	a := testee.Attrs()
	if a == nil || a.Mode != mode {
		t.Error("Attrs() invalid")
	}
	hash, err := testee.Hash()
	require.NoError(t, err)
	if hash != "" {
		t.Error("hash != ''")
	}
	testee.WriteFiles("/dir", writerMock)
	actual := strings.Join(writerMock.written, "\n")
	expected := "d /dir"
	if actual != expected {
		t.Errorf("expected %q but was %q", expected, actual)
	}
}
