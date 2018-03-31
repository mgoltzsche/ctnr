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

func ParseUser(v string) (r User, err error) {
	s := strings.Split(v, ":")
	l := len(s)
	r.User = strings.TrimSpace(s[0])
	switch {
	case l == 2 && strings.TrimSpace(s[1]) != "":
		r.Group = strings.TrimSpace(s[1])
	case l == 1 && r.User != "":
		r.Group = r.User
	default:
		err = errors.Errorf("expected USER[:GROUP] but was %q", v)
	}
	return
}

func (u User) Resolve(rootfs string) (r UserIds, err error) {
	if u.User == "" {
		return r, errors.New("lookup uid: no user specified")
	}
	uid, e := parseUint(u.User)
	if e == nil {
		r.Uid = uid
	}
	if e != nil || u.Group == "" {
		r, err = LookupUser(u.User, rootfs)
		if err != nil {
			return
		}
	}
	if u.Group != "" {
		r.Gid, err = LookupGid(u.Group, rootfs)
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
