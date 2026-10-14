package loglevel_test

import (
	"log/slog"
	"testing"

	"github.com/we-apranik/apranik/module/api/clash/internal/loglevel"
)

func TestNameFoldsEightSeveritiesIntoClashsFour(t *testing.T) {
	for level, want := range map[slog.Level]string{
		slog.LevelDebug: "debug", slog.Level(-8): "debug",
		slog.LevelInfo: "info", slog.Level(2): "info",
		slog.LevelWarn: "warning", slog.Level(6): "warning",
		slog.LevelError: "error", slog.Level(10): "error", slog.Level(12): "error", slog.Level(14): "error",
	} {
		if got := loglevel.Name(level); got != want {
			t.Errorf("Name(%d) = %q, want %q", level, got, want)
		}
	}
}

func TestMinimumReadsClashNamesAndSilentIsAboveEveryRecord(t *testing.T) {
	for name, want := range map[string]slog.Level{
		"debug": slog.LevelDebug, "info": slog.LevelInfo, "warning": slog.LevelWarn, "error": slog.LevelError, "silent": loglevel.Silent,
	} {
		if got, ok := loglevel.Minimum(name); !ok || got != want {
			t.Errorf("Minimum(%q) = %d, %v; want %d", name, got, ok, want)
		}
	}
	if loglevel.Silent <= slog.Level(14) {
		t.Errorf("Silent = %d is not above the most severe level", loglevel.Silent)
	}
	for _, name := range []string{"", "loud", "INFO", "warn"} {
		if _, ok := loglevel.Minimum(name); ok {
			t.Errorf("Minimum(%q) was accepted", name)
		}
	}
}
