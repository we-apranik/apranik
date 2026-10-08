// Package loglevel translates between core's log levels (slog.Level numbers,
// with RFC 5424's eight severities between them) and the four names Clash uses.
// A module cannot import core's logging package, so it works from the numbers.
package loglevel

import "log/slog"

// Silent is above every level a record has, so a subscription at it sees nothing.
const Silent = slog.Level(1000)

// Name is the Clash name of a level: records below info are debug, info and
// notice are info, warnings are warning, and error and above are error.
func Name(level slog.Level) string {
	switch {
	case level < slog.LevelInfo:
		return "debug"
	case level < slog.LevelWarn:
		return "info"
	case level < slog.LevelError:
		return "warning"
	default:
		return "error"
	}
}

// Minimum is the least severe level a Clash level name asks for.
func Minimum(name string) (slog.Level, bool) {
	switch name {
	case "debug":
		return slog.LevelDebug, true
	case "info":
		return slog.LevelInfo, true
	case "warning":
		return slog.LevelWarn, true
	case "error":
		return slog.LevelError, true
	case "silent":
		return Silent, true
	default:
		return 0, false
	}
}
