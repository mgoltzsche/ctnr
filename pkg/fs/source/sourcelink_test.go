package source

import (
	"testing"

	"github.com/mgoltzsche/ctnr/pkg/fs"
	"github.com/mgoltzsche/ctnr/pkg/fs/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func assertSourceWriteWithHardlinkSupport(t *testing.T, testee fs.Source, expectedDefaultLine string) {
	for _, c := range []struct {
		written  map[fs.Source]string
		expected string
	}{
		{map[fs.Source]string{}, expectedDefaultLine},
		{map[fs.Source]string{testee: "/existing-file"}, "/file hlink=/existing-file"},
	} {
		writerMock := testutils.NewWriterMock(t, fs.AttrsAll)
		err := testee.Write("/file", "", writerMock, c.written)
		require.NoError(t, err)
		if !assert.Equal(t, []string{c.expected}, writerMock.Written, "hardlink support") {
			t.FailNow()
		}
	}
}
