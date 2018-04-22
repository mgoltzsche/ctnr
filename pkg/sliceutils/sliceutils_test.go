package sliceutils

import (
	"fmt"
	"testing"
)

func TestAddToSet(t *testing.T) {
	for _, c := range []struct {
		value    []string
		addv     string
		expected string
		added    bool
	}{
		{nil, "a", "[a]", true},
		{[]string{"a"}, "b", "[a b]", true},
		{[]string{"a"}, "", "[a ]", true},
		{[]string{"a"}, "a", "[a]", false},
	} {
		added := AddToSet(&c.value, c.addv)
		a := fmt.Sprintf("%+v", c.value)
		e := fmt.Sprintf("%+v", c.expected)
		if a != e {
			t.Errorf("AddToSet(%+v, %q) => %s but expected %s", c.value, c.addv, a, e)
		}
		if added != c.added {
			t.Errorf("AddToSet(%+v, %q) returned %v but expected %v", c.value, c.addv, added, c.added)
		}
	}
}

func TestRemoveFromSet(t *testing.T) {
	for _, c := range []struct {
		value    []string
		remv     string
		expected string
		removed  bool
	}{
		{nil, "a", "[]", false},
		{[]string{"a", "b"}, "c", "[a b]", false},
		{[]string{"a", "b"}, "b", "[a]", true},
		{[]string{"a"}, "a", "[]", true},
		{[]string{"a", ""}, "", "[a]", true},
	} {
		removed := RemoveFromSet(&c.value, c.remv)
		a := fmt.Sprintf("%+v", c.value)
		e := fmt.Sprintf("%+v", c.expected)
		if a != e {
			t.Errorf("RemoveFromSet(%+v, %q) => %s but expected %s", c.value, c.remv, a, e)
		}
		if removed != c.removed {
			t.Errorf("RemoveFromSet(%+v, %q) returned %v but expected %v", c.value, c.remv, removed, c.removed)
		}
	}
}

func TestContains(t *testing.T) {
	for _, c := range []struct {
		l         []string
		v         string
		contained bool
	}{
		{nil, "a", false},
		{[]string{"a"}, "b", false},
		{[]string{"a"}, "", false},
		{[]string{"a"}, "a", true},
		{[]string{"a", "b"}, "b", true},
		{[]string{"a", ""}, "", true},
	} {
		a := Contains(c.l, c.v)
		if a != c.contained {
			t.Errorf("Contains(%+v, %q) must return true", c.l, c.v)
		}
	}
}
