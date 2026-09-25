//go:build !windows && !plan9

package logging

import (
	"context"
	"fmt"
	"log/slog"
	"log/syslog"
	"slices"
	"strings"
)

// NewSyslogSink writes to the system log daemon. The eight severities map onto
// syslog's priorities one for one, which is why they are RFC 5424's. network
// and address are passed to syslog.Dial; empty means the local daemon.
func NewSyslogSink(network, address, tag string, level Level) (slog.Handler, error) {
	var writer *syslog.Writer
	var err error
	if network == "" {
		writer, err = syslog.New(syslog.LOG_INFO|syslog.LOG_DAEMON, tag)
	} else {
		writer, err = syslog.Dial(network, address, syslog.LOG_INFO|syslog.LOG_DAEMON, tag)
	}
	if err != nil {
		return nil, fmt.Errorf("logging: syslog: %w", err)
	}

	return &syslogSink{writer: writer, level: level}, nil
}

type syslogSink struct {
	writer *syslog.Writer
	level  Level
	attrs  []slog.Attr
	groups []string
}

func (s *syslogSink) Enabled(_ context.Context, level slog.Level) bool {
	return level >= s.level
}

func (s *syslogSink) Handle(_ context.Context, record slog.Record) error {
	var line strings.Builder
	line.WriteString(record.Message)

	write := func(attr slog.Attr) bool {
		fmt.Fprintf(&line, " %s=%v", attr.Key, attr.Value)
		return true
	}
	for _, attr := range s.attrs {
		write(attr)
	}
	record.Attrs(write)

	switch {
	case record.Level >= LevelEmergency:
		return s.writer.Emerg(line.String())
	case record.Level >= LevelAlert:
		return s.writer.Alert(line.String())
	case record.Level >= LevelCritical:
		return s.writer.Crit(line.String())
	case record.Level >= LevelError:
		return s.writer.Err(line.String())
	case record.Level >= LevelWarning:
		return s.writer.Warning(line.String())
	case record.Level >= LevelNotice:
		return s.writer.Notice(line.String())
	case record.Level >= LevelInfo:
		return s.writer.Info(line.String())
	default:
		return s.writer.Debug(line.String())
	}
}

func (s *syslogSink) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return s
	}
	derived := *s
	derived.attrs = append(slices.Clip(s.attrs), nest(s.groups, attrs)...)
	return &derived
}

func (s *syslogSink) WithGroup(name string) slog.Handler {
	if name == "" {
		return s
	}
	derived := *s
	derived.groups = append(slices.Clip(s.groups), name)
	return &derived
}

func (s *syslogSink) Close() error { return s.writer.Close() }
