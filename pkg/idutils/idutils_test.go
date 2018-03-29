package idutils

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLookupGid(t *testing.T) {
	rootfs := newTestRootfs(t, "/etc/group", `
		root:!:0:
		
		daemon:x:1:usera
		# comment
		testgroup:x:7777:usera,userb
	`)
	defer os.RemoveAll(rootfs)
	dir, err := ioutil.TempDir("", "cntnr-test-gid-")
	require.NoError(t, err)
	defer os.RemoveAll(dir)
	for _, c := range []struct {
		name    string
		gid     uint
		rootfs  string
		resolve bool
	}{
		{"root", 0, rootfs, true},
		{"daemon", 1, rootfs, true},
		{"testgroup", 7777, rootfs, true},
		{"9", 9, dir, true},
		{"unknowngroup", 0, rootfs, false},
	} {
		gid, err := LookupGid(c.name, c.rootfs)
		if c.resolve {
			if err == nil {
				assert.Equal(t, c.gid, gid)
			} else {
				t.Errorf("returned error for group %q: %s", c.name, err)
			}
		} else if err == nil {
			t.Errorf("lookup of group %q should fail", c.name)
		}
	}
}

func TestLookupUser(t *testing.T) {
	rootfs := newTestRootfs(t, "/etc/passwd", `
		root:x:0:0:root:/root:/bin/bash
		
		daemon:x:1:1:daemon:/usr/sbin:/usr/sbin/nologin
		# comment
		myuser:x:9000:7:bin:/bin:/usr/sbin/nologin
	`)
	defer os.RemoveAll(rootfs)
	dir, err := ioutil.TempDir("", "cntnr-test-usr-")
	require.NoError(t, err)
	defer os.RemoveAll(dir)
	for _, c := range []struct {
		name    string
		uid     uint
		gid     uint
		rootfs  string
		resolve bool
	}{
		{"root", 0, 0, rootfs, true},
		{"daemon", 1, 1, rootfs, true},
		{"myuser", 9000, 7, rootfs, true},
		{"9", 9, 9, dir, true},
		{"unknownuser", 0, 0, rootfs, false},
	} {
		u, err := LookupUser(c.name, c.rootfs)
		if c.resolve {
			if err == nil {
				assert.Equal(t, c.uid, u.Uid, "did not resolve uid properly: "+c.name)
				assert.Equal(t, c.gid, u.Gid, "did not resolve gid properly: "+c.name)
			} else {
				t.Errorf("returned error for user %q: %s", c.name, err)
			}
		} else if err == nil {
			t.Errorf("lookup of user %q should fail", c.name)
		}
	}
}

func TestParseUserExpr(t *testing.T) {
	for _, c := range []struct {
		expr  string
		user  string
		group string
		valid bool
	}{
		{"root", "root", "root", true},
		{"root:root", "root", "root", true},
		{"root:root", "root", "root", true},
		{"root:mygroup", "root", "mygroup", true},
		{"", "", "", false},
	} {
		u, err := ParseUser(c.expr)
		if c.valid {
			if err == nil {
				assert.Equal(t, c.user, u.User, "did not resolve user properly: "+c.expr)
				assert.Equal(t, c.group, u.Group, "did not resolve group properly: "+c.expr)
			} else {
				t.Errorf("returned error for user %q: %s", c.expr, err)
			}
		} else if err == nil {
			t.Errorf("lookup of user %q should fail", c.expr)
		}
	}
}

func newTestRootfs(t *testing.T, file, content string) string {
	rootfs, err := ioutil.TempDir("", "cntnr-idutils-test-")
	require.NoError(t, err)
	file = filepath.Join(rootfs, file)
	err = os.Mkdir(filepath.Dir(file), 0755)
	require.NoError(t, err)
	err = ioutil.WriteFile(file, []byte(content), 0600)
	require.NoError(t, err)
	return rootfs
}
