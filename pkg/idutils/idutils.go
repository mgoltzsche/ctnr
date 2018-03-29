package idutils

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/pkg/errors"
)

// Looks up a user by uid or name in the rootfs' /etc/passwd
func LookupUser(usr string, rootfs string) (r UserIds, err error) {
	field := 0
	if r.Uid, err = parseUint(usr); err == nil {
		field = 2
	}
	file := filepath.Join(rootfs, "etc", "passwd")
	u := userLookup(r)
	found, err := lookupColonFileLine(&u, usr, field, 7, file)
	if err != nil && os.IsNotExist(err) && field == 2 {
		r.Gid = r.Uid
		return r, nil
	}
	if !found {
		err = errors.Errorf("user %q not found in container's /etc/passwd", usr)
	}
	return UserIds(u), err
}

// Looks up a gid by a given name in the rootfs' /etc/group
func LookupGid(grp, rootfs string) (gid uint, err error) {
	if gid, err = parseUint(grp); err == nil {
		return
	}
	file := filepath.Join(rootfs, "etc", "group")
	g := gidLookup(gid)
	found, err := lookupColonFileLine(&g, grp, 0, 4, file)
	if err == nil && !found {
		err = errors.Errorf("group %q not found in container's /etc/group", grp)
	}
	return uint(g), err
}

// TODO: maybe also read the user's groups and put them into the spec process's AdditionalGroups? (but then read /etc/groups only once!)
//       ... or delete this method with its gidsByUserLookup
// Looks up all associated gids by a given username in the given rootfs' /etc/groups
func LookupGidsByUser(name string, rootfs string) (gids []uint, err error) {
	matches := gidsByUserLookup(gids)
	file := filepath.Join(rootfs, "etc", "group")
	_, err = lookupColonFileLine(&matches, name, 3, 4, file)
	return []uint(matches), errors.Wrapf(err, "lookup gids of user %q", name)
}

type matcher interface {
	match(value string, fields []string, field int) (bool, error)
}

type userLookup UserIds

func (s *userLookup) match(value string, fields []string, field int) (found bool, err error) {
	if value == fields[field] {
		u := (*UserIds)(s)
		if u.Uid, err = parseUint(fields[2]); err == nil {
			u.Gid, err = parseUint(fields[3])
		}
		return true, errors.Wrapf(err, "invalid /etc/passwd line: %+v", fields)
	}
	return false, nil
}

type gidLookup uint

func (s *gidLookup) match(gname string, fields []string, field int) (found bool, err error) {
	if gname == fields[field] {
		*(*uint)(s), err = parseUint(fields[2])
		return true, errors.Wrapf(err, "invalid /etc/group line: %+v", fields)
	}
	return false, nil
}

type gidsByUserLookup []uint

func (s *gidsByUserLookup) match(usrname string, fields []string, field int) (bool, error) {
	for _, e := range strings.Split(fields[field], ",") {
		if usrname == strings.TrimSpace(e) {
			gids := (*[]uint)(s)
			gid, err := parseUint(fields[2])
			*gids = append(*gids, gid)
			return false, errors.Wrapf(err, "invalid /etc/group line: %+v", fields)
		}
	}
	return false, nil
}

func lookupColonFileLine(m matcher, value string, field, nFields int, file string) (found bool, err error) {
	f, err := os.Open(file)
	if err != nil {
		return
	}
	defer f.Close()
	bs := bufio.NewScanner(f)
	for bs.Scan() {
		line := string(bytes.TrimSpace(bs.Bytes()))
		if len(line) == 0 || line[0] == '#' {
			continue
		}
		fields := strings.Split(line, ":")
		if len(fields) < nFields {
			continue
		}
		if found, err = m.match(value, fields, field); found || err != nil {
			return
		}
	}
	err = bs.Err()
	return
}

func parseUint(v string) (uint, error) {
	uid, err := strconv.Atoi(v)
	if err != nil {
		return 0, err
	}
	if uid < 0 {
		return 0, errors.Errorf("supported range exceeded: %s", v)
	}
	return uint(uid), nil
}
