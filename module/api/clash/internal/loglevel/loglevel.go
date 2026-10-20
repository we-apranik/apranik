package loglevel

import "log/slog"

// Silent is above every level a record has, so a subscription at it sees nothing.
const Silent = slog.Level(1000)

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
