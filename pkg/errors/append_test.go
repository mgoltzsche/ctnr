package errors

import (
	"testing"

	orgerrors "github.com/pkg/errors"
)

func TestAppend(t *testing.T) {
	a := orgerrors.New("a")
	b := orgerrors.New("b")
	c := orgerrors.Wrap(b, "c")
	d := &multiError{a, []error{c}}
	e := orgerrors.Wrap(d, "e")
	for _, c := range []struct {
		a        error
		b        error
		expected error
		cause    error
	}{
		{nil, nil, nil, nil},
		{a, nil, a, a},
		{nil, b, b, b},
		{c, nil, c, b},
		{nil, c, c, b},
		{a, c, d, a},
		{d, b, &multiError{a, []error{c, b}}, a},
		{e, b, &multiError{e, []error{b}}, a},
	} {
		actual := Append(c.a, c.b)
		if actual == nil && c.expected != nil || actual != nil && c.expected == nil || actual != nil && actual.Error() != c.expected.Error() {
			t.Errorf("Append(%q, %q) != %q but %q", c.a, c.b, c.expected, actual)
		}
		if cause := orgerrors.Cause(actual); cause != c.cause {
			t.Errorf("cause not preserved for Append(%q, %q): expected %q but was %q", c.a, c.b, c.cause, cause)
		}
		if c.a != nil && c.b != nil {
			if len(actual.(*multiError).Additional()) == 0 {
				t.Errorf("Append(%q, %q).Additional() did return 0 entries", c.a, c.b)
			}
		}
	}
}
