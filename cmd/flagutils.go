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
	//"github.com/mgoltzsche/cntnr/generate"
)

func parseBool(s string) (bool, error) {
	b, err := strconv.ParseBool(s)
	if err != nil {
		err = fmt.Errorf("Only 'true' or 'false' are accepted values")
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
			return fmt.Errorf("Expected option value format: NAME=VALUE")
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
