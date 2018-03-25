package usergroupname

import (
	"bufio"
	"bytes"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/pkg/errors"
)

func LookupIdFromFile(name string, dbfile string) (id uint, err error) {
	if name == "" {
		return id, errors.New("id lookup: empty name provided")
	}
	if id, err = parseUint(name); err != nil {
		var f *os.File
		if f, err = os.Open(dbfile); err == nil {
			defer f.Close()
			id, err = lookupId(name, f)
			err = errors.Wrapf(err, "id lookup in %s", dbfile)
		} else {
			err = errors.Wrap(err, "id lookup")
		}
	}
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

func lookupId(name string, r io.Reader) (id uint, err error) {
	bs := bufio.NewScanner(r)
	for bs.Scan() {
		line := string(bytes.TrimSpace(bs.Bytes()))
		if len(line) == 0 || line[0] == '#' {
			continue
		}
		parts := strings.SplitN(line, ":", 4)
		if len(parts) < 4 {
			continue
		}
		if name == parts[0] {
			uid, e := parseUint(parts[2])
			if e != nil {
				return id, errors.Wrapf(e, "invalid line %q", line)
			}
			return uid, nil
		}
	}
	if err = bs.Err(); err == nil {
		err = errors.Errorf("%q not found", name)
	} else {
		err = errors.Errorf("lookup %q: %s", name, err)
	}
	return
}
