package errors

import (
	"fmt"
	"testing"

	orgerrors "github.com/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestTyped(t *testing.T) {
	tname := "testtype"
	for _, err := range []error{Typed(tname, "test msg"), Typedf(tname, "test %s", "msg")} {
		require.Error(t, err)
		s := err.Error()
		if s != "test msg" {
			t.Errorf("Error() returned %q", s)
		}
		sf := fmt.Sprintf("%+v", err)
		if s == sf || len(sf) == 0 {
			t.Error("Format() not implemented properly")
		}
		if HasType(err, "unknowntype") {
			t.Errorf("HasType(unknown, type) must return false")
		}
		if !HasType(err, tname) {
			t.Errorf("HasType(err, type) must return true")
		}
		err = orgerrors.Wrap(err, "wrapped")
		if !HasType(err, tname) {
			t.Errorf("HasType(Wrap(err), type) must return true")
		}
	}
}
