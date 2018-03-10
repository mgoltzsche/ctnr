package model

import (
	"testing"

	"github.com/mgoltzsche/cntnr/pkg/sliceutils"
)

func TestMountParser(t *testing.T) {
	assertParseMount("type=bind,src=./src,dst=./dst,opt=nodev,opt=mode=0755,readonly", t)
	assertParseMount("src=./src,dst=./dst,opt=nodev,opt=mode=0755,readonly", t)
	assertParseMount("source=./src,dst=./dst,opt=nodev,opt=mode=0755,readonly", t)
	assertParseMount("src=./src,destination=./dst,opt=nodev,opt=mode=0755,readonly", t)
	assertParseMount("src=./src,target=./dst,opt=nodev,opt=mode=0755,readonly", t)
	assertParseMount("src=./src,dst=./dst,volume-opt=nodev,opt=mode=0755,readonly", t)
	assertParseMount("./src:./dst:nodev:mode=0755:ro", t)
}

func TestMountString(t *testing.T) {
	e := "type=bind,src=./src,dst=./dst,opt=nodev,opt=mode=0755,opt=ro"
	a := mockVolumeMount().String()
	if a != e {
		t.Errorf("invalid String() result %q. expected %q", a, e)
	}
}

func TestMountStringConversion(t *testing.T) {
	assertParseMount(mockVolumeMount().String(), t)
}

func mockVolumeMount() (m VolumeMount) {
	m.Type = "bind"
	m.Source = "./src"
	m.Target = "./dst"
	m.Options = []string{"nodev", "mode=0755", "ro"}
	return
}

func assertParseMount(expr string, t *testing.T) {
	m, err := ParseMount(expr)
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
	if len(m.Options) != 3 || !sliceutils.Contains(m.Options, "nodev") ||
		!sliceutils.Contains(m.Options, "ro") ||
		!sliceutils.Contains(m.Options, "mode=0755") {
		t.Errorf("invalid options %v parsed from %q", m.Options, expr)
		fail = true
	}
	if fail {
		t.FailNow()
	}
}
