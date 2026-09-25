package logging

import (
	"context"
	"log/slog"
)

// Logger is slog.Logger plus the four severities slog has no method for.
type Logger struct {
	*slog.Logger
}

func newLogger(handler slog.Handler) *Logger {
	return &Logger{Logger: slog.New(handler)}
}

// Discard saves every component a nil check before every call.
func Discard() *Logger { return newLogger(slog.DiscardHandler) }

// With re-wraps so a tagged logger keeps all eight levels.
func (l *Logger) With(args ...any) *Logger {
	return &Logger{Logger: l.Logger.With(args...)}
}

func (l *Logger) WithGroup(name string) *Logger {
	return &Logger{Logger: l.Logger.WithGroup(name)}
}

func (l *Logger) Notice(msg string, args ...any) {
	l.Log(context.Background(), LevelNotice, msg, args...)
}

func (l *Logger) Critical(msg string, args ...any) {
	l.Log(context.Background(), LevelCritical, msg, args...)
}

func (l *Logger) Alert(msg string, args ...any) {
	l.Log(context.Background(), LevelAlert, msg, args...)
}

func (l *Logger) Emergency(msg string, args ...any) {
	l.Log(context.Background(), LevelEmergency, msg, args...)
}
