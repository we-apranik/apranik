// Package logging gives every component one leveled logger: eight RFC 5424
// severities over log/slog, fanned out to any number of sinks. Core passes the
// logger down, so a module logs without deciding where the output goes.
package logging

import (
	"errors"
	"io"
	"log/slog"
)

type Config struct {
	// Zero value is LevelInfo.
	Level Level

	// Empty means the logger is silent. That is the default on purpose: a log
	// naming what a user reached is evidence on a seized device.
	Outputs []Output
}

type System struct {
	logger *Logger
	sinks  []slog.Handler
}

// New is where outputs are opened. On a failure partway through, what was
// opened is closed again, so a bad third output does not leak the first two.
func New(config Config) (*System, error) {
	sinks := make([]slog.Handler, 0, len(config.Outputs))
	for _, output := range config.Outputs {
		sink, err := output.open(config.Level)
		if err != nil {
			closeSinks(sinks)
			return nil, err
		}
		sinks = append(sinks, sink)
	}

	var handler slog.Handler
	switch len(sinks) {
	case 0:
		handler = slog.DiscardHandler
	case 1:
		handler = sinks[0]
	default:
		handler = &multiHandler{sinks: sinks}
	}

	return &System{logger: newLogger(handler), sinks: sinks}, nil
}

func (s *System) Logger() *Logger { return s.logger }

func (s *System) Close() error { return closeSinks(s.sinks) }

func closeSinks(sinks []slog.Handler) error {
	var failures []error
	for _, sink := range sinks {
		if closer, ok := sink.(io.Closer); ok {
			if err := closer.Close(); err != nil {
				failures = append(failures, err)
			}
		}
	}
	return errors.Join(failures...)
}
