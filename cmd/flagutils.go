// Copyright © 2017 Max Goltzsche
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cmd

import (
	"fmt"
	"strconv"
	"strings"

	shellwords "github.com/mattn/go-shellwords"
	"github.com/mgoltzsche/cntnr/model"
	"github.com/pkg/errors"
)

func parseBool(s string) (bool, error) {
	b, err := strconv.ParseBool(s)
	if err != nil {
		err = errors.New("Only 'true' or 'false' are valid values")
	}
	return b, err
}

func parseStringEntries(s string) (r []string, err error) {
	if s == "" {
		return nil, nil
	}
	// TODO: fix parsing of cat asdf | sed ... > asd
	// (currently parse ignores everything after the cat cmd silently)
	return shellwords.Parse(s)
}

func addStringEntries(s string, r *[]string) error {
	e, err := parseStringEntries(s)
	if err != nil || len(e) == 0 {
		return err
	}
	*r = append(*r, e...)
	return nil
}

func entriesToString(l []string) string {
	s := ""
	if len(l) > 0 {
		for _, e := range l {
			s += fmt.Sprintf(" %q", e)
		}
	}
	return strings.Trim(s, " ")
}

func addMapEntries(s string, r *map[string]string) error {
	entries, err := shellwords.Parse(s)
	if err != nil {
		return err
	}
	if *r == nil {
		*r = map[string]string{}
	}
	for _, e := range entries {
		sp := strings.SplitN(e, "=", 2)
		k := strings.Trim(sp[0], " ")
		if len(sp) != 2 || k == "" || strings.Trim(sp[1], " ") == "" {
			return errors.New("Expected option value format: NAME=VALUE")
		}
		(*r)[k] = strings.Trim(sp[1], " ")
	}
	return nil
}

func mapToString(m map[string]string) string {
	s := ""
	if len(m) > 0 {
		for k, v := range m {
			s += strings.Trim(fmt.Sprintf(" %q", k+"="+v), " ")
		}
	}
	return s
}

// Parses a docker-like mount expression or falls back to the docker-like volume expression.
// See https://docs.docker.com/storage/bind-mounts/#choosing-the--v-or-mount-flag
func ParseMount(expr string) (r model.VolumeMount, err error) {
	// Parse kv pairs
	r.Type = model.MOUNT_TYPE_BIND
	r.Options = []string{}
	for _, e := range strings.Split(expr, ",") {
		kv := strings.SplitN(e, "=", 2)
		k := strings.ToLower(strings.Trim(kv[0], " "))
		v := ""
		if len(kv) == 2 {
			v = strings.Trim(kv[1], " ")
		}
		switch {
		case k == "type":
			r.Type = model.MountType(v)
		case k == "source" || k == "src":
			r.Source = v
		case k == "destination" || k == "dst" || k == "target":
			r.Target = v
		case k == "readonly":
			r.Options = append(r.Options, "ro")
		case k == "volume-opt" || k == "opt":
			r.Options = append(r.Options, v)
		default:
			return r, errors.Errorf("unsupported mount key %q", k)
		}
	}
	if r.Type == "" {
		if r.Source == "" {
			return r, errors.New("no mount type specified")
		}
		if r.IsNamedVolume() {
			r.Type = "volume"
		} else {
			r.Type = "bind"
		}
	}
	if r.Target == "" {
		err = errors.Errorf("no volume mount target specified but %q", expr)
	}
	return
}

// Parses a volume mount.
// See https://docs.docker.com/storage/volumes/#choose-the--v-or---mount-flag
func ParseBindMount(expr string) (r model.VolumeMount, err error) {
	r.Type = model.MOUNT_TYPE_BIND
	r.Options = []string{}
	s := strings.SplitN(expr, ":", 3)
	switch len(s) {
	case 1:
		r.Source = ""
		r.Target = s[0]
	case 2:
		r.Source = s[0]
		r.Target = s[1]
	default:
		r.Source = s[0]
		r.Target = s[1]
		r.Options = strings.Split(s[2], ",")
	}
	if r.Target == "" {
		err = errors.Errorf("no volume mount target specified but %q", expr)
	}
	return
}
