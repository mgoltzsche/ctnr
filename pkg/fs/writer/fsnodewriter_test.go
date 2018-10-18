package writer

// TODO: remove when it is clear that this is tested in fsbuilder
/*
import (
	"testing"

	"github.com/mgoltzsche/ctnr/pkg/fs/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFsNodeWriter(t *testing.T) {
	testee := NewFsNodeWriter(files.NewFS(), types.NoopWriter())
	expectedFs := newFsNodeTree(t, false)
	err := expectedFs.Write(testee)
	require.NoError(t, err)
	expectedNodes := testutils.MockWrites(t, expectedFs).written
	actualNodes := mockWrites(t, testee.FS()).written
	if !assert.Equal(t, expectedNodes, actualNodes, "writer.FS() != nodes") {
		t.FailNow()
	}
	err = expectedFs.Write(testee)
	require.NoError(t, err)
	actualNodes = mockWrites(t, testee.FS()).written
	if !assert.Equal(t, expectedNodes, actualNodes, "writer.FS() != nodes (2nd run)") {
		t.FailNow()
	}
}*/
