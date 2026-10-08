package sink

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/The-Apranik/Apranik/core/logging"
)

// DefaultMemoryCapacity is the ring size a MemoryOutput gets when it names none.
const DefaultMemoryCapacity = 256

// Each output below describes where records go, without opening anything: the
// logging manager opens them in logging.New, so a configuration can be built
// and checked on a machine that could never run it. They satisfy
// logging.Output.

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
		return fmt.Errorf(`sink: color needs the %q format`, string(FormatHuman))
	}
	return nil
}

func (o StderrOutput) Open(level logging.Level) (slog.Handler, error) {
	return NewStderr(level, o.format(), o.Color), nil
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
		return errors.New("sink: a file output needs a path")
	}
	return o.format().validate()
}

func (o FileOutput) Open(level logging.Level) (slog.Handler, error) {
	return NewFile(o.Path, level, o.format())
}

type MemoryOutput struct {
	// Zero means DefaultMemoryCapacity.
	Capacity int
}

func (o MemoryOutput) Validate() error {
	if o.Capacity < 0 {
		return errors.New("sink: a memory output's capacity must not be negative")
	}
	return nil
}

func (o MemoryOutput) Open(level logging.Level) (slog.Handler, error) {
	capacity := o.Capacity
	if capacity == 0 {
		capacity = DefaultMemoryCapacity
	}
	return NewMemory(capacity, level), nil
}

type SyslogOutput struct {
	// Passed to syslog.Dial; both empty means the local daemon.
	Network string
	Address string
	Tag     string
}

func (SyslogOutput) Validate() error { return nil }

func (o SyslogOutput) Open(level logging.Level) (slog.Handler, error) {
	return NewSyslog(o.Network, o.Address, o.Tag, level)
}
