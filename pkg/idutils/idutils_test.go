package usergroupname

import (
	"bytes"
	"io/ioutil"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testIdMap = `
		root:!:0:
		daemon:x:1:
		myuser:x:3:
	`

func TestLookupIdFromFile(t *testing.T) {
	f, err := ioutil.TempFile("", "cntnr-idutils-test-")
	require.NoError(t, err)
	defer os.Remove(f.Name())
	defer f.Close()
	err = ioutil.WriteFile(f.Name(), []byte(testIdMap), 0600)
	require.NoError(t, err)
	id, err := LookupIdFromFile("myuser", f.Name())
	require.NoError(t, err)
	assert.Equal(t, uint(3), id)
}

func TestLookupIdFromFileWithUid(t *testing.T) {
	id, err := LookupIdFromFile("1000", "/dbfile")
	require.NoError(t, err)
	assert.Equal(t, uint(1000), id)
}

func TestLookupIdShouldResolve(t *testing.T) {
	for _, c := range []struct {
		name string
		id   uint
	}{
		{"root", 0},
		{"daemon", 1},
		{"myuser", 3},
	} {
		aid, err := lookupId(c.name, bytes.NewReader([]byte(testIdMap)))
		require.NoError(t, err)
		assert.Equal(t, c.id, aid, "did not resolve name "+c.name)
	}
}

func TestLookupIdNonExistingShouldFail(t *testing.T) {
	if _, err := lookupId("nonexisting", bytes.NewReader([]byte{})); err == nil {
		t.Error("lookup of non existing name should fail")
	}
}
