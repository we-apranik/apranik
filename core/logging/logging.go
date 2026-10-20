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
	Level Level

	// Empty means the logger is silent. That is the default on purpose: a log
	// naming what a user reached is evidence on a seized device.
	Outputs []Output
}

type LoggerManager struct {
	logger *Logger
	sinks  []slog.Handler
	hub    *subscriptionHub
}

// New is where outputs are opened. On a failure partway through, what was
// opened is closed again, so a bad third output does not leak the first two.
func New(config Config) (*LoggerManager, error) {
	sinks := make([]slog.Handler, 0, len(config.Outputs))
	for _, output := range config.Outputs {
		sink, err := output.Open(config.Level)
		if err != nil {
			closeSinks(sinks)
			return nil, err
		}
		sinks = append(sinks, sink)
	}

	// Always in the chain, whatever the outputs are: a component's logger is
	// derived once, at build time, and a sink added afterwards would never be
	// seen by it. With no subscriber the hub is disabled, so no outputs still
	// means silent.
	hub := newSubscriptionHub()
	sinks = append(sinks, hub)

	var handler slog.Handler
	if len(sinks) == 1 {
		handler = hub
	} else {
		handler = &multiHandler{sinks: sinks}
	}

	return &LoggerManager{logger: newLogger(handler), sinks: sinks, hub: hub}, nil
}

func (s *LoggerManager) SubscribeToLog(minimumLevel Level, bufferedRecords int) (*Subscription, error) {
	return s.hub.add(minimumLevel, bufferedRecords)
}

func (s *LoggerManager) Logger() *Logger { return s.logger }

func (s *LoggerManager) Close() error { return closeSinks(s.sinks) }

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
