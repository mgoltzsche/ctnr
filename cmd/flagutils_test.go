package cmd

import (
	"testing"

	"github.com/mgoltzsche/ctnr/model"
	"github.com/mgoltzsche/ctnr/pkg/sliceutils"
)

func TestParseBool(t *testing.T) {
	for _, c := range []struct {
		value    string
		expected bool
		valid    bool
	}{
		{"true", true, true},
		{"1", true, true},
		{"false", false, true},
		{"0", false, true},
		{"", false, false},
		{"x", false, false},
	} {
		a, err := parseBool(c.value)
		if c.valid {
			if err != nil {
				t.Errorf("%q returned error: %s", c.value, err)
				continue
			}
			if c.expected != a {
				t.Errorf("%q => %v, %v != %v", c.value, a, a, c.expected)
			}
		} else {
			if err == nil {
				t.Errorf("%q should return error", c.value)
			}
		}
	}
}

func TestEntriesToString(t *testing.T) {
	for _, c := range []struct {
		value    []string
		expected string
	}{
		{nil, ""},
		{[]string{"a"}, "\"a\""},
		{[]string{"a", "b"}, "\"a\" \"b\""},
		{[]string{"a", "b \"c\""}, "\"a\" \"b \\\"c\\\"\""},
	} {
		a := entriesToString(c.value)
		if c.expected != a {
			t.Errorf("%q => %s, %s != %s", c.value, a, a, c.expected)
		}
	}
}

func TestParseStringEntries(t *testing.T) {
	for _, c := range []struct {
		value    string
		expected []string
		valid    bool
	}{
		{"", nil, true},
		{"a", []string{"a"}, true},
		{"$a", []string{"$a"}, true},
		{"${a}", []string{"${a}"}, true},
		{"a b", []string{"a", "b"}, true},
		{"\"a\" b", []string{"a", "b"}, true},
		{"\"a\" \"b\"", []string{"a", "b"}, true},
		{"\"a\" \"b c\"", []string{"a", "b c"}, true},
		{"\"a\" \"b \\\"c\\\"\"", []string{"a", "b \"c\""}, true},
		{"\"a\" \"b ${c}\"", []string{"a", "b ${c}"}, true},
		{"\"a\" \"b $c\"", []string{"a", "b $c"}, true},
		{"\"a b", nil, false},
	} {
		a, err := parseStringEntries(c.value)
		if c.valid {
			if err != nil {
				t.Errorf("%q returned error: %s", c.value, err)
				continue
			}
			astr := entriesToString(a)
			estr := entriesToString(c.expected)
			if astr != estr {
				t.Errorf("%q => %s, %s != %s", c.value, astr, astr, estr)
			}
		} else {
			if err == nil {
				t.Errorf("%q should return error", c.value)
			}
		}
	}
}

func TestParseMount(t *testing.T) {
	assertParseMount(t, "type=bind,src=./src,dst=./dst,opt=nodev,opt=mode=0755,readonly")
	assertParseMount(t, "src=./src,dst=./dst,opt=nodev,opt=mode=0755,readonly")
	assertParseMount(t, "source=./src,dst=./dst,opt=nodev,opt=mode=0755,readonly")
	assertParseMount(t, "src=./src,destination=./dst,opt=nodev,opt=mode=0755,readonly")
	assertParseMount(t, "src=./src,target=./dst,opt=nodev,opt=mode=0755,readonly")
	assertParseMount(t, "src=./src,dst=./dst,volume-opt=nodev,opt=mode=0755,readonly")
}

func TestParseBindMount(t *testing.T) {
	assertParseBindMount(t, "./src:./dst:nodev,mode=0755,ro")
}

func TestMountStringConversion(t *testing.T) {
	assertParseMount(t, mockVolumeMount().String())
}

func mockVolumeMount() (m model.VolumeMount) {
	m.Type = "bind"
	m.Source = "./src"
	m.Target = "./dst"
	m.Options = []string{"nodev", "mode=0755", "ro"}
	return
}

func assertParseMount(t *testing.T, expr string) {
	assertMountParser(t, expr, ParseMount)
}

func assertParseBindMount(t *testing.T, expr string) {
	assertMountParser(t, expr, ParseBindMount)
}

func assertMountParser(t *testing.T, expr string, testee func(string) (model.VolumeMount, error)) {
	m, err := testee(expr)
	if err != nil {
		t.Error(err)
		t.FailNow()
		return
	}
	fail := false
	if m.Type != "bind" {
		t.Errorf("expected type %q parsed from %q", m.Type, expr)
		fail = true
	}
	if m.Source != "./src" {
		t.Errorf("invalid source %q parsed from %q", m.Source, expr)
		fail = true
	}
	if m.Target != "./dst" {
		t.Errorf("invalid destination %q parsed from %q", m.Target, expr)
		fail = true
	}
	if len(m.Options) != 3 ||
		!sliceutils.Contains(m.Options, "nodev") ||
		!sliceutils.Contains(m.Options, "ro") ||
		!sliceutils.Contains(m.Options, "mode=0755") {
		t.Errorf("invalid options %v parsed from %q", m.Options, expr)
		fail = true
	}
	if fail {
		t.FailNow()
	}
}
