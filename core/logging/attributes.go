package logging

import "log/slog"

func NestInGroups(groups []string, attrs []slog.Attr) []slog.Attr {
	for index := len(groups) - 1; index >= 0; index-- {
		attrs = []slog.Attr{slog.Any(groups[index], slog.GroupValue(attrs...))}
	}
	return attrs
}
