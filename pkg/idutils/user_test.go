package idutils

import (
	"io/ioutil"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseUserExpr(t *testing.T) {
	for _, c := range []struct {
		expr  string
		user  string
		group string
		valid bool
	}{
		{"root", "root", "root", true},
		{"root:root", "root", "root", true},
		{"root:root ", "root", "root", true},
		{"  root : root ", "root", "root", true},
		{"root:mygroup", "root", "mygroup", true},
		{"", "", "", false},
	} {
		u, err := ParseUser(c.expr)
		if c.valid {
			require.NoError(t, err, "unexpected error for "+c.expr)
			assert.Equal(t, c.user, u.User, "did not resolve user properly: "+c.expr)
			assert.Equal(t, c.group, u.Group, "did not resolve group properly: "+c.expr)
		} else if err == nil {
			t.Errorf("lookup of user %q should fail", c.expr)
		}
	}
}

func TestUserResolve(t *testing.T) {
	rootfs := newTestRootfs(t)
	defer os.RemoveAll(rootfs)
	dir, err := ioutil.TempDir("", "cntnr-test-usr-")
	require.NoError(t, err)
	defer os.RemoveAll(dir)
	for _, c := range []struct {
		user    string
		group   string
		uid     uint
		gid     uint
		rootfs  string
		resolve bool
	}{
		{"daemon", "daemon", 1, 1, rootfs, true},
		{"myuser", "", 9000, 7, rootfs, true},
		{"myuser", "testgroup", 9000, 7777, rootfs, true},
		{"myuser", "9000", 9000, 9000, rootfs, true},
		{"9000", "", 9000, 7, rootfs, true},
		{"9000", "testgroup", 9000, 7777, rootfs, true},
		{"300", "testgroup", 300, 7777, rootfs, true},
		{"300", "300", 300, 300, dir, true},
		{"unknownusr", "unknowngrp", 0, 0, rootfs, false},
		{"", "", 0, 0, rootfs, false},
	} {
		u := User{c.user, c.group}
		r, err := u.Resolve(c.rootfs)
		if c.resolve {
			require.NoError(t, err, "unexpected error for "+u.String())
			assert.Equal(t, c.uid, r.Uid, "did not resolve uid properly: "+c.user)
			assert.Equal(t, c.gid, r.Gid, "did not resolve gid properly: "+u.String())
		} else if err == nil {
			t.Errorf("lookup of user %q should fail", u)
		}
	}
}
