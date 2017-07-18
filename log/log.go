package log

import (
	"io"
	stdlog "log"
)

type Logger interface {
	Print(...interface{})
	Printf(string, ...interface{})
	Println(...interface{})
}

func NewStdLogger(out io.Writer) Logger {
	return stdlog.New(out, "", 0)
}

type nopLogger struct{}

func NewNopLogger() Logger {
	return &nopLogger{}
}

func (l *nopLogger) Print(...interface{})          {}
func (l *nopLogger) Printf(string, ...interface{}) {}
func (l *nopLogger) Println(...interface{})        {}
