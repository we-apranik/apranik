package logging

import (
	"errors"
	"fmt"
	"log/slog"
)

// DefaultMemoryCapacity is the ring size a MemoryOutput gets when it names none.
const DefaultMemoryCapacity = 256

// Output describes where records go, without opening anything. The logging
// system opens outputs in New, so a configuration can be built and checked on
// a machine that could never run it.
//
// The set is closed: the open method is unexported, so the four types below
// plus HandlerOutput are the only outputs there are.
type Output interface {
	Validate() error
	open(level Level) (slog.Handler, error)
}

type StderrOutput struct {
	// Empty means FormatLogfmt: programs that run Apranik capture and parse
	// its stderr more often than a person reads it.
	Format Format

	// Only with FormatHuman.
	Color bool
}

func (o StderrOutput) format() Format {
	if o.Format == "" {
		return FormatLogfmt
	}
	return o.Format
}

func (o StderrOutput) Validate() error {
	if err := o.format().validate(); err != nil {
		return err
	}
	if o.Color && o.format() != FormatHuman {
		return fmt.Errorf(`logging: color needs the %q format`, string(FormatHuman))
	}
	return nil
}

func (o StderrOutput) open(level Level) (slog.Handler, error) {
	return NewStderrSink(level, o.format(), o.Color), nil
}

type FileOutput struct {
	Path string

	// Empty means FormatJSON.
	Format Format
}

func (o FileOutput) format() Format {
	if o.Format == "" {
		return FormatJSON
	}
	return o.Format
}

func (o FileOutput) Validate() error {
	if o.Path == "" {
		return errors.New("logging: a file output needs a path")
	}
	return o.format().validate()
}

func (o FileOutput) open(level Level) (slog.Handler, error) {
	return NewFileSink(o.Path, level, o.format())
}

type MemoryOutput struct {
	// Zero means DefaultMemoryCapacity.
	Capacity int
}

func (o MemoryOutput) Validate() error {
	if o.Capacity < 0 {
		return errors.New("logging: a memory output's capacity must not be negative")
	}
	return nil
}

func (o MemoryOutput) open(level Level) (slog.Handler, error) {
	capacity := o.Capacity
	if capacity == 0 {
		capacity = DefaultMemoryCapacity
	}
	return NewMemorySink(capacity, level), nil
}

type SyslogOutput struct {
	// Passed to syslog.Dial; both empty means the local daemon.
	Network string
	Address string
	Tag     string
}

func (SyslogOutput) Validate() error { return nil }

func (o SyslogOutput) open(level Level) (slog.Handler, error) {
	return NewSyslogSink(o.Network, o.Address, o.Tag, level)
}

// HandlerOutput is an already-built handler, for an embedder that brings its
// own sink. A *MemorySink passed this way stays readable by the embedder.
type HandlerOutput struct {
	Handler slog.Handler
}

func (o HandlerOutput) Validate() error {
	if o.Handler == nil {
		return errors.New("logging: a handler output needs a handler")
	}
	return nil
}

func (o HandlerOutput) open(Level) (slog.Handler, error) {
	return o.Handler, nil
}
