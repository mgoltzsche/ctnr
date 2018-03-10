package model

import (
	"testing"

	"github.com/mgoltzsche/cntnr/log"
)

func TestVariableSubstitution(t *testing.T) {
	testenv := map[string]string{
		"VAR1": "dyn1",
		"VAR2": "dyn2",
	}
	cases := []struct {
		input    string
		expected string
	}{
		{"static-$VAR1", "static-dyn1"},
		{"static-$VAR1-XY", "static-dyn1-XY"},
		{"static-$VAR1-$VAR2", "static-dyn1-dyn2"},
		{"static-$VAR1-$VAR2-$VAR3", "static-dyn1-dyn2-"},
		{"static-${VAR1}", "static-dyn1"},
		{"static-${VAR1}-XY", "static-dyn1-XY"},
		{"static-${VAR1}-${VAR2}", "static-dyn1-dyn2"},
		{"static-${VAR1}-${VAR2}-${VAR3}", "static-dyn1-dyn2-"},
		{"static-${VAR1}-${VAR2}-${VAR3-defaultval}", "static-dyn1-dyn2-defaultval"},
		{"static-${VAR1}-${VAR2}-${VAR3:-defaultval}", "static-dyn1-dyn2-defaultval"},
	}
	for _, c := range cases {
		testee := NewSubstitution(testenv, log.NewNopLogger())
		actual := testee(c.input)
		if actual != c.expected {
			t.Errorf("%q should be replaced with %q but was %q", c.input, c.expected, actual)
		}
	}
}
