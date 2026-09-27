package logging

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"os"
	"sync"
)

// NewStderrSink writes to stderr in format. NO_COLOR, when set, turns color
// off whatever the config says (https://no-color.org).
func NewStderrSink(level Level, format Format, color bool) slog.Handler {
	color = color && os.Getenv("NO_COLOR") == ""
	return newFormatHandler(os.Stderr, format, level, color)
}

const (
	ansiReset          = "\x1b[0m"
	ansiDim            = "\x1b[2m"
	ansiBoldRed        = "\x1b[1;31m"
	ansiRed            = "\x1b[31m"
	ansiGreen          = "\x1b[32m"
	ansiYellow         = "\x1b[33m"
	ansiBlue           = "\x1b[34m"
	ansiGray           = "\x1b[90m"
	terminalTimeFormat = "2006-01-02T15:04:05.000Z07:00"

	// Wide enough for EMERGENCY, so messages line up.
	levelColumnWidth = 9
)

func levelColor(level Level) string {
	switch {
	case level >= LevelCritical:
		return ansiBoldRed
	case level >= LevelError:
		return ansiRed
	case level >= LevelWarning:
		return ansiYellow
	case level >= LevelNotice:
		return ansiBlue
	case level >= LevelInfo:
		return ansiGreen
	default:
		return ansiGray
	}
}

// terminalSinkOutput is shared by a sink and every sink derived from it through
// WithAttrs or WithGroup, so their lines never interleave.
type terminalSinkOutput struct {
	mutex          sync.Mutex
	writer         io.Writer
	attributesLine bytes.Buffer
}

// terminalSink writes the time, level and message itself and lets a TextHandler
// render the attributes, so groups and WithAttrs behave exactly as in the
// plain sink.
type terminalSink struct {
	output            *terminalSinkOutput
	level             Level
	color             bool
	attributeRenderer slog.Handler
}

func newTerminalSink(writer io.Writer, level Level, color bool) *terminalSink {
	output := &terminalSinkOutput{writer: writer}
	return &terminalSink{
		output: output,
		level:  level,
		color:  color,
		attributeRenderer: slog.NewTextHandler(&output.attributesLine, &slog.HandlerOptions{
			Level:       level,
			ReplaceAttr: dropBuiltInAttributes,
		}),
	}
}

func dropBuiltInAttributes(groups []string, attr slog.Attr) slog.Attr {
	if len(groups) == 0 && (attr.Key == slog.TimeKey || attr.Key == slog.LevelKey || attr.Key == slog.MessageKey) {
		return slog.Attr{}
	}
	return attr
}

func (s *terminalSink) Enabled(_ context.Context, level Level) bool {
	return level >= s.level
}

func (s *terminalSink) Handle(ctx context.Context, record slog.Record) error {
	s.output.mutex.Lock()
	defer s.output.mutex.Unlock()

	s.output.attributesLine.Reset()
	if err := s.attributeRenderer.Handle(ctx, record); err != nil {
		return err
	}
	attributes := bytes.TrimRight(s.output.attributesLine.Bytes(), "\n")

	var line bytes.Buffer
	if !record.Time.IsZero() {
		line.WriteString(s.paint(ansiDim, record.Time.Format(terminalTimeFormat)) + " ")
	}
	levelName := LevelName(record.Level)
	line.WriteString(s.paint(levelColor(record.Level), levelName))
	for padding := len(levelName); padding < levelColumnWidth; padding++ {
		line.WriteByte(' ')
	}
	line.WriteByte(' ')
	line.WriteString(record.Message)
	if len(attributes) > 0 {
		line.WriteByte(' ')
		line.Write(attributes)
	}
	line.WriteByte('\n')

	_, err := s.output.writer.Write(line.Bytes())
	return err
}

func (s *terminalSink) paint(ansiColor, text string) string {
	if !s.color {
		return text
	}
	return ansiColor + text + ansiReset
}

func (s *terminalSink) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &terminalSink{output: s.output, level: s.level, color: s.color, attributeRenderer: s.attributeRenderer.WithAttrs(attrs)}
}

func (s *terminalSink) WithGroup(name string) slog.Handler {
	return &terminalSink{output: s.output, level: s.level, color: s.color, attributeRenderer: s.attributeRenderer.WithGroup(name)}
}
