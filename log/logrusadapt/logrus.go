package logrusadapt

import (
	"github.com/mgoltzsche/cntnr/log"
	"github.com/sirupsen/logrus"
)

func NewDebugLogger(l logrus.FieldLogger) log.FieldLogger {
	return debugLogger{l}
}

func NewErrorLogger(l logrus.FieldLogger) log.FieldLogger {
	return errorLogger{l}
}

func NewInfoLogger(l logrus.FieldLogger) log.FieldLogger {
	return infoLogger{l}
}

func NewWarnLogger(l logrus.FieldLogger) log.FieldLogger {
	return warnLogger{l}
}

type errorLogger struct {
	logrus.FieldLogger
}

func (l errorLogger) Println(o ...interface{}) {
	l.Errorln(o...)
}

func (l errorLogger) Printf(f string, o ...interface{}) {
	l.Errorf(f, o...)
}

func (l errorLogger) WithField(name string, value interface{}) log.FieldLogger {
	return errorLogger{l.FieldLogger.WithField(name, value)}
}

type debugLogger struct {
	logrus.FieldLogger
}

func (l debugLogger) Println(o ...interface{}) {
	l.Debugln(o...)
}

func (l debugLogger) Printf(f string, o ...interface{}) {
	l.Debugf(f, o...)
}

func (l debugLogger) WithField(name string, value interface{}) log.FieldLogger {
	return debugLogger{l.FieldLogger.WithField(name, value)}
}

type infoLogger struct {
	logrus.FieldLogger
}

func (l infoLogger) Println(o ...interface{}) {
	l.Infoln(o...)
}

func (l infoLogger) Printf(f string, o ...interface{}) {
	l.Infof(f, o...)
}

func (l infoLogger) WithField(name string, value interface{}) log.FieldLogger {
	return infoLogger{l.FieldLogger.WithField(name, value)}
}

type warnLogger struct {
	logrus.FieldLogger
}

func (l warnLogger) Println(o ...interface{}) {
	l.Warnln(o...)
}

func (l warnLogger) Printf(f string, o ...interface{}) {
	l.Warnf(f, o...)
}

func (l warnLogger) WithField(name string, value interface{}) log.FieldLogger {
	return warnLogger{l.FieldLogger.WithField(name, value)}
}
