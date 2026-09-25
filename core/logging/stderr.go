package logging

import (
	"log/slog"
	"os"
)

func NewStderrSink(level Level) slog.Handler {
	return slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level:       level,
		ReplaceAttr: replaceLevel,
	})
}
