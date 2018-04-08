package store

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormWriter(t *testing.T) {
	input := `
		###
		#          user: root
		#       machine: ubuntu-x6
		#          tree: /tmp/cntnr-test-fsdiff-052992796
		#          date: Sat Apr 7 16:10:19 2018
		#      keywords: size,type,uid,gid,mode,link,nlink,tar_time,sha256digest,xattr
		
		# .
		/set type=file nlink=1 mode=0664 uid=0 gid=0
		. size=4096 type=dir mode=0700 nlink=3 tar_time=1523117419.000000000
		
		# dir
		dir size=4096 type=dir mode=0755 nlink=2 tar_time=1523117419.000000000
		    file104308171 size=0 mode=0600 tar_time=1523117419.000000000 sha256digest=e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855
		..
		..
	`
	var out bytes.Buffer
	testee := normWriter(&out)
	b := []byte(input)
	normalized := make([]byte, 0, len(b))
	lines := bytes.Split(b, []byte("\n"))
	for _, line := range lines {
		_, err := testee.Write(line)
		require.NoError(t, err)
		line = bytes.TrimSpace(line)
		if len(line) > 0 && line[0] != '#' {
			normalized = append(append(normalized, line...), []byte("\n")...)
		}
	}
	assert.Equal(t, string(normalized), out.String())
}
