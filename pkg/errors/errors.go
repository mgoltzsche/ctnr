package errors

import (
	orgerrors "github.com/pkg/errors"
)

func Wrapd(err *error, msg string) {
	*err = orgerrors.Wrap(*err, msg)
}

func Wrapdf(err *error, fmt string, args ...interface{}) {
	*err = orgerrors.Wrapf(*err, fmt, args)
}
