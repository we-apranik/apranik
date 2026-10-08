package logging

import (
	"fmt"
	"log/slog"
	"strings"
)

// Level is slog.Level. An alias, not a defined type, so it passes straight into
// slog.HandlerOptions without conversions.
type Level = slog.Level

// The RFC 5424 severities, most verbose first. slog's built-ins are spaced
// four apart so levels can be added between them; the other four sit in gaps.
const (
	LevelDebug     = slog.LevelDebug // severity 7
	LevelInfo      = slog.LevelInfo  // severity 6
	LevelNotice    = Level(2)        // severity 5
	LevelWarning   = slog.LevelWarn  // severity 4
	LevelError     = slog.LevelError // severity 3
	LevelCritical  = Level(10)       // severity 2
	LevelAlert     = Level(12)       // severity 1
	LevelEmergency = Level(14)       // severity 0
)

var levelNames = []struct {
	level Level
	name  string
}{
	{LevelDebug, "DEBUG"},
	{LevelInfo, "INFO"},
	{LevelNotice, "NOTICE"},
	{LevelWarning, "WARNING"},
	{LevelError, "ERROR"},
	{LevelCritical, "CRITICAL"},
	{LevelAlert, "ALERT"},
	{LevelEmergency, "EMERGENCY"},
}

func LevelName(level Level) string {
	for _, known := range levelNames {
		if known.level == level {
			return known.name
		}
	}
	return level.String()
}

func ParseLevel(name string) (Level, error) {
	wanted := strings.ToUpper(strings.TrimSpace(name))
	for _, known := range levelNames {
		if known.name == wanted {
			return known.level, nil
		}
	}
	return 0, fmt.Errorf("logging: %q is not a severity level", name)
}

// slog prints a level it does not know as an offset from one it does, so
// LevelNotice would reach a sink as "INFO+2". Every sink installs this.
func ReplaceLevelWithName(_ []string, attr slog.Attr) slog.Attr {
	if attr.Key == slog.LevelKey {
		if level, ok := attr.Value.Any().(slog.Level); ok {
			attr.Value = slog.StringValue(LevelName(level))
		}
	}
	return attr
}
