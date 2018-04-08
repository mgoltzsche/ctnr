package idutils

import (
	"strings"

	"github.com/pkg/errors"
)

type UserIds struct {
	Uid uint
	Gid uint
}

type User struct {
	User  string
	Group string
}

func ParseUser(v string) (r User) {
	s := strings.SplitN(v, ":", 2)
	r.User = strings.TrimSpace(s[0])
	if len(s) == 2 {
		r.Group = strings.TrimSpace(s[1])
	}
	return
}

func (u User) ToIds() (r UserIds, err error) {
	usr := u.User
	grp := u.Group
	if usr == "" {
		usr = "0"
		if grp == "" {
			grp = "0"
		}
	}
	uid, ue := parseUint(usr)
	gid, ge := parseUint(grp)
	if grp == "" || ue != nil || ge != nil {
		err = errors.New("cannot derive uid/gid from " + u.String() + " without rootfs")
	}
	r.Uid = uid
	r.Gid = gid
	return
}

func (u User) Resolve(rootfs string) (r UserIds, err error) {
	if r, err = u.ToIds(); err == nil {
		return
	}

	usr := u.User
	grp := u.Group
	if usr == "" {
		usr = "0"
	}
	uid, e := parseUint(usr)
	if e == nil {
		r.Uid = uid
	}
	if e != nil || grp == "" {
		r, err = LookupUser(usr, rootfs)
		if err != nil {
			return
		}
	}
	if grp != "" {
		r.Gid, err = LookupGid(grp, rootfs)
	}
	return
}

func (u User) String() string {
	s := u.User
	if u.Group != "" {
		s += ":" + u.Group
	}
	return s
}
