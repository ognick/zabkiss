package logger

import "fmt"

// withLogger оборачивает Logger и добавляет постоянные поля к каждому вызову.
type withLogger struct {
	base   Logger
	fields []any
}

// With возвращает Logger, который добавляет fields в начало каждого лог-вызова.
func With(base Logger, fields ...any) Logger {
	return &withLogger{base: base, fields: fields}
}

func (l *withLogger) Info(msg string, args ...any) {
	l.base.Info(msg, concat(l.fields, args)...)
}

func (l *withLogger) Error(msg string, args ...any) {
	l.base.Error(msg, concat(l.fields, args)...)
}

func (l *withLogger) Debug(msg string, args ...any) {
	l.base.Debug(msg, concat(l.fields, args)...)
}

func (l *withLogger) Warn(msg string, args ...any) {
	l.base.Warn(msg, concat(l.fields, args)...)
}

func (l *withLogger) Infof(format string, args ...any) {
	l.base.Info(fmt.Sprintf(format, args...), l.fields...)
}

func (l *withLogger) Errorf(format string, args ...any) {
	l.base.Error(fmt.Sprintf(format, args...), l.fields...)
}

func concat(a, b []any) []any {
	out := make([]any, len(a)+len(b))
	copy(out, a)
	copy(out[len(a):], b)
	return out
}
