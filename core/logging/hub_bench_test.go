package logging

import (
	"context"
	"io"
	"log/slog"
	"testing"
)

func benchmarkLoggerManager(b *testing.B, outputLevel Level) *LoggerManager {
	b.Helper()
	system, err := New(Config{Level: outputLevel, Outputs: []Output{HandlerOutput{Handler: slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: outputLevel})}}})
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { system.Close() })
	return system
}

func BenchmarkLogCallAtADisabledLevel(b *testing.B) {
	logger := benchmarkLoggerManager(b, LevelWarning).Logger().With("outbound", "auto")
	b.ReportAllocs()
	for b.Loop() {
		logger.Debug("Group member chosen", "member", "server-a")
	}
}

func BenchmarkEnabledCheckAtADisabledLevel(b *testing.B) {
	logger := benchmarkLoggerManager(b, LevelWarning).Logger()
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		logger.Enabled(ctx, LevelDebug)
	}
}
