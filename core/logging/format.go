package logging

import (
	"fmt"
	"io"
	"log/slog"
)

// Format is how a sink writes records. Each output has its own default.
type Format string

const (
	FormatLogfmt Format = "logfmt"
	FormatJSON   Format = "json"

	// FormatHuman is aligned, and may be colored, for a person at a terminal.
	FormatHuman Format = "human"
)

func (f Format) validate() error {
	switch f {
	case FormatLogfmt, FormatJSON, FormatHuman:
		return nil
	default:
		return fmt.Errorf(`logging: %q is not a log format; want "logfmt", "json" or "human"`, string(f))
	}
}

func newFormatHandler(writer io.Writer, format Format, level Level, color bool) slog.Handler {
	options := &slog.HandlerOptions{Level: level, ReplaceAttr: replaceLevel}
	switch format {
	case FormatJSON:
		return slog.NewJSONHandler(writer, options)
	case FormatHuman:
		return newTerminalSink(writer, level, color)
	default:
		return slog.NewTextHandler(writer, options)
	}
}
