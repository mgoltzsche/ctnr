package model

import (
	"testing"
)

func TestMountString(t *testing.T) {
	e := "type=bind,src=./src,dst=./dst,opt=nodev,opt=mode=0755,opt=ro"
	m := VolumeMount{}
	m.Type = "bind"
	m.Source = "./src"
	m.Target = "./dst"
	m.Options = []string{"nodev", "mode=0755", "ro"}
	a := m.String()
	if a != e {
		t.Errorf("invalid String() result %q. expected %q", a, e)
	}
}
