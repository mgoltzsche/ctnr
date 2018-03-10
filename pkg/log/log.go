package log

type Loggers struct {
	Error FieldLogger
	Warn  FieldLogger
	Info  FieldLogger
	Debug FieldLogger
}

func (l Loggers) WithField(name string, value interface{}) Loggers {
	return Loggers{
		Error: l.Error.WithField(name, value),
		Warn:  l.Warn.WithField(name, value),
		Info:  l.Info.WithField(name, value),
		Debug: l.Debug.WithField(name, value),
	}
}

type Logger interface {
	Printf(string, ...interface{})
	Println(...interface{})
}

type FieldLogger interface {
	Logger
	WithField(name string, value interface{}) FieldLogger
}

type nopLogger struct{}

func NewNopLogger() FieldLogger {
	return &nopLogger{}
}

func (l *nopLogger) Printf(string, ...interface{})             {}
func (l *nopLogger) Println(...interface{})                    {}
func (l *nopLogger) WithField(string, interface{}) FieldLogger { return l }
