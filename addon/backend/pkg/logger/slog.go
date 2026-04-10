package logger

import (
	"fmt"
	"log/slog"
)

type SlogAdapter struct {
	log *slog.Logger
}

func NewSlogAdapter(log *slog.Logger) *SlogAdapter {
	return &SlogAdapter{log: log}
}

func (a *SlogAdapter) Info(msg string, args ...any) {
	a.log.Info(msg, args...)
}

func (a *SlogAdapter) Error(msg string, args ...any) {
	a.log.Error(msg, args...)
}

func (a *SlogAdapter) Debug(msg string, args ...any) {
	a.log.Debug(msg, args...)
}

func (a *SlogAdapter) Warn(msg string, args ...any) {
	a.log.Warn(msg, args...)
}

func (a *SlogAdapter) Infof(format string, args ...any) {
	a.log.Info(fmt.Sprintf(format, args...))
}

func (a *SlogAdapter) Errorf(format string, args ...any) {
	a.log.Error(fmt.Sprintf(format, args...))
}
