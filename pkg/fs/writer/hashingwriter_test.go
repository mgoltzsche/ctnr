package writer

import (
	"io/ioutil"
	"testing"

	"github.com/mgoltzsche/cntnr/pkg/fs"
	"github.com/mgoltzsche/cntnr/pkg/fs/source"
	"github.com/mgoltzsche/cntnr/pkg/fs/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHashingWriter(t *testing.T) {
	mockWriter := &mockHashingWriterDelegate{t, testutils.NewWriterMock(t, fs.AttrsHash)}
	testee := NewHashingWriter(mockWriter)
	src, err := testee.File("/file", source.NewSourceFile(fs.NewReadableBytes([]byte("testcontent")), fs.FileAttrs{Mode: 0640}))
	require.NoError(t, err)
	assert.NotNil(t, src, "returned source")
	a, err := src.DeriveAttrs()
	require.NoError(t, err, "result.DeriveAttrs()")
	if !assert.Equal(t, "sha256:25edaa1f62bd4f2a7e4aa7088cf4c93449c1881af03434bfca027f1f82d69dba", a.Hash) {
		t.FailNow()
	}
	expected := []string{
		"/file type=file mode=640 hash=sha256:25edaa1f62bd4f2a7e4aa7088cf4c93449c1881af03434bfca027f1f82d69dba",
	}
	if !assert.Equal(t, expected, mockWriter.Written) {
		t.FailNow()
	}
}

type mockHashingWriterDelegate struct {
	t *testing.T
	*testutils.WriterMock
}

func (w *mockHashingWriterDelegate) File(path string, src fs.FileSource) (fs.Source, error) {
	r, err := src.Reader()
	require.NoError(w.t, err, "Reader()")
	_, err = ioutil.ReadAll(r)
	require.NoError(w.t, err, "ReadAll(Reader())")
	err = r.Close()
	require.NoError(w.t, err, "reader.Close()")
	return w.WriterMock.File(path, src)
}
