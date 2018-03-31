package idutils

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	etcPasswd = `
		root:x:0:0:root:/root:/bin/bash
		
		daemon:x:1:1:daemon:/usr/sbin:/usr/sbin/nologin
		# comment
		myuser:x:9000:7:bin:/bin:/usr/sbin/nologin
	`
	etcGroup = `
		root:!:0:
		
		daemon:x:1:usera
		# comment
		testgroup:x:7777:usera,userb
	`
)

func TestLookupGid(t *testing.T) {
	rootfs := newTestRootfs(t)
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
			require.NoError(t, err, "unexpected error for group "+c.name)
			assert.Equal(t, c.gid, gid)
		} else if err == nil {
			t.Errorf("lookup of group %q should fail", c.name)
		}
	}
}

func TestLookupUser(t *testing.T) {
	rootfs := newTestRootfs(t)
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
			require.NoError(t, err, "unexpected error for user "+c.name)
			assert.Equal(t, c.uid, u.Uid, "did not resolve uid properly: "+c.name)
			assert.Equal(t, c.gid, u.Gid, "did not resolve gid properly: "+c.name)
		} else if err == nil {
			t.Errorf("lookup of user %q should fail", c.name)
		}
	}
}

func newTestRootfs(t *testing.T) string {
	rootfs, err := ioutil.TempDir("", "cntnr-idutils-test-")
	require.NoError(t, err)
	err = os.Mkdir(filepath.Join(rootfs, "etc"), 0755)
	require.NoError(t, err)
	err = ioutil.WriteFile(filepath.Join(rootfs, "etc", "passwd"), []byte(etcPasswd), 0600)
	require.NoError(t, err)
	err = ioutil.WriteFile(filepath.Join(rootfs, "etc", "group"), []byte(etcGroup), 0600)
	require.NoError(t, err)
	return rootfs
}
