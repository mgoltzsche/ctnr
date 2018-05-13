package files

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSourceSymlink(t *testing.T) {
	writerMock := newWriterMock(t)
	testee := sourceSymlink{FileAttrs{Link: "../linkdest"}}
	if testee.Type() != TypeSymlink {
		t.Error("Type() != TypeLink")
	}
	a := testee.Attrs()
	if a == nil {
		t.Error("Attrs() should not be nil")
	}
	hash, err := testee.Hash()
	require.NoError(t, err)
	if hash != "" {
		t.Error("symlink should not provide hash")
	}
	err = testee.WriteFiles("/linkx", writerMock)
	require.NoError(t, err)
	actual := strings.Join(writerMock.written, "\n")
	expected := "L /linkx"
	if actual != expected {
		t.Errorf("expected %q but was %q", expected, actual)
	}
}
