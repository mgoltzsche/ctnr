package idutils

import (
	"os"
	"strconv"
	"strings"

	exterrors "github.com/mgoltzsche/ctnr/pkg/errors"
	idmap "github.com/openSUSE/umoci/pkg/idtools"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/pkg/errors"
)

var (
	MapIdentity IdMappings = idIdentity("identity")
	MapRootless            = NewIdMappings([]specs.LinuxIDMapping{{uint32(os.Geteuid()), 0, 1}}, []specs.LinuxIDMapping{{uint32(os.Getegid()), 0, 1}})
)

type UserIds struct {
	Uid uint
	Gid uint
}

func (u UserIds) IsZero() bool {
	return u.Uid == 0 && u.Gid == 0
}

type IdMappings interface {
	UidToHost(int) (int, error)
	GidToHost(int) (int, error)
	UidToContainer(int) (int, error)
	GidToContainer(int) (int, error)
}

type idIdentity string

func (m idIdentity) UidToHost(uid int) (int, error)      { return uid, nil }
func (m idIdentity) GidToHost(gid int) (int, error)      { return gid, nil }
func (m idIdentity) UidToContainer(uid int) (int, error) { return uid, nil }
func (m idIdentity) GidToContainer(gid int) (int, error) { return gid, nil }

type idMappings struct {
	uidMappings []specs.LinuxIDMapping
	gidMappings []specs.LinuxIDMapping
}

func NewIdMappings(uidMappings, gidMappings []specs.LinuxIDMapping) IdMappings {
	return &idMappings{uidMappings, gidMappings}
}

func (m *idMappings) UidToHost(uid int) (muid int, err error) {
	muid, err = idmap.ToHost(uid, m.uidMappings)
	err = errors.Wrap(err, "map uid to host")
	return
}

func (m *idMappings) GidToHost(gid int) (mgid int, err error) {
	mgid, err = idmap.ToHost(gid, m.gidMappings)
	err = errors.Wrap(err, "map uid to host")
	return
}

func (m *idMappings) UidToContainer(uid int) (muid int, err error) {
	muid, err = idmap.ToContainer(uid, m.uidMappings)
	err = errors.Wrap(err, "map uid to host")
	return
}

func (m *idMappings) GidToContainer(gid int) (mgid int, err error) {
	mgid, err = idmap.ToContainer(gid, m.gidMappings)
	err = errors.Wrap(err, "map uid to host")
	return
}

func (u *UserIds) ToHost(m IdMappings) (r UserIds, err error) {
	uid, err := m.UidToHost(int(u.Uid))
	gid, err2 := m.GidToHost(int(u.Gid))
	if err = exterrors.Append(err, err2); err == nil {
		r.Uid = uint(uid)
		r.Gid = uint(gid)
	}
	return
}

func (u *UserIds) ToContainer(m IdMappings) (r UserIds, err error) {
	uid, err := m.UidToContainer(int(u.Uid))
	gid, err2 := m.GidToContainer(int(u.Gid))
	if err = exterrors.Append(err, err2); err == nil {
		r.Uid = uint(uid)
		r.Gid = uint(gid)
	}
	return
}

func (u *UserIds) String() string {
	return strconv.Itoa(int(u.Uid)) + ":" + strconv.Itoa(int(u.Gid))
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
