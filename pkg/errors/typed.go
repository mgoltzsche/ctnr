package errors

import (
	"fmt"

	orgerrors "github.com/pkg/errors"
)

type typed struct {
	t     string
	cause error
}

func Typed(typeName, msg string) error {
	return &typed{typeName, orgerrors.New(msg)}
}

func Typedf(typeName, format string, o ...interface{}) error {
	return &typed{typeName, orgerrors.Errorf(format, o...)}
}

func (e *typed) Type() string {
	return e.t
}

func (e *typed) Error() string {
	return e.cause.Error()
}

func (e *typed) Format(s fmt.State, verb rune) {
	type formatter interface {
		Format(s fmt.State, verb rune)
	}
	e.cause.(formatter).Format(s, verb)
}

func HasType(err error, typeName string) bool {
	type causer interface {
		Cause() error
	}
	type typed interface {
		Type() string
	}
	if terr, ok := err.(typed); ok && terr.Type() == typeName {
		return true
	}
	if cerr, ok := err.(causer); ok && cerr.Cause() != nil {
		return HasType(cerr.Cause(), typeName)
	}
	return false
}
