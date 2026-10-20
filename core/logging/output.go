package logging

import (
	"errors"
	"log/slog"
)

// Output describes where records go, without opening anything. The logging
// system opens outputs in New, so a configuration can be built and checked on
// a machine that could never run it.
//
// The package sink holds the outputs core offers: stderr, a file, memory and
// syslog. HandlerOutput below wraps a handler an embedder already built.
type Output interface {
	Validate() error
	Open(level Level) (slog.Handler, error)
}

type HandlerOutput struct {
	Handler slog.Handler
}

func (o HandlerOutput) Validate() error {
	if o.Handler == nil {
		return errors.New("logging: a handler output needs a handler")
	}
	return nil
}

func (o HandlerOutput) Open(Level) (slog.Handler, error) {
	return o.Handler, nil
}
