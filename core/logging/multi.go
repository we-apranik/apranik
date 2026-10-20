package logging

import (
	"context"
	"errors"
	"log/slog"
)

type multiHandler struct {
	sinks []slog.Handler
}

func (m *multiHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, sink := range m.sinks {
		if sink.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (m *multiHandler) Handle(ctx context.Context, record slog.Record) error {
	var failures []error
	for _, sink := range m.sinks {
		if !sink.Enabled(ctx, record.Level) {
			continue
		}
		if err := sink.Handle(ctx, record.Clone()); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func (m *multiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := make([]slog.Handler, len(m.sinks))
	for index, sink := range m.sinks {
		next[index] = sink.WithAttrs(attrs)
	}
	return &multiHandler{sinks: next}
}

func (m *multiHandler) WithGroup(name string) slog.Handler {
	next := make([]slog.Handler, len(m.sinks))
	for index, sink := range m.sinks {
		next[index] = sink.WithGroup(name)
	}
	return &multiHandler{sinks: next}
}
