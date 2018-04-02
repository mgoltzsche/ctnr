package errors

import (
	"strings"
)

type multiError struct {
	cause      error
	additional []error
}

func (e *multiError) Error() string {
	s := e.cause.Error() + "\n  Additional errors:"
	for _, err := range e.additional {
		s += "\n  * " + strings.Replace(err.Error(), "\n", "\n    ", -1)
	}
	return s
}

func (e *multiError) Cause() error {
	return e.cause
}

func (e *multiError) Additional() []error {
	return e.additional
}

// Creates a new error containing the additional errors
// while preserving the original /pkg/error chain
func Append(err error, add error) (r error) {
	if err == nil {
		return add
	} else if add == nil {
		return err
	}
	var errs []error
	if merr, ok := err.(*multiError); ok {
		err = merr.Cause()
		errs = append(merr.additional, add)
	} else {
		errs = []error{add}
	}
	return &multiError{err, errs}
}
