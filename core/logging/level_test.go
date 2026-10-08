package logging

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestTheEightSeveritiesAreOrderedAndDistinct(t *testing.T) {
	ordered := []Level{
		LevelDebug, LevelInfo, LevelNotice, LevelWarning,
		LevelError, LevelCritical, LevelAlert, LevelEmergency,
	}

	for index := 1; index < len(ordered); index++ {
		if ordered[index] <= ordered[index-1] {
			t.Errorf("%s (%d) does not outrank %s (%d)",
				LevelName(ordered[index]), ordered[index],
				LevelName(ordered[index-1]), ordered[index-1])
		}
	}
}

func TestLevelNameParseLevelRoundTrip(t *testing.T) {
	for _, level := range []Level{
		LevelDebug, LevelInfo, LevelNotice, LevelWarning,
		LevelError, LevelCritical, LevelAlert, LevelEmergency,
	} {
		name := LevelName(level)
		parsed, err := ParseLevel(name)
		if err != nil {
			t.Errorf("ParseLevel(%q): %v", name, err)
			continue
		}
		if parsed != level {
			t.Errorf("ParseLevel(%q) = %d, want %d", name, parsed, level)
		}
	}

	if _, err := ParseLevel("chatty"); err == nil {
		t.Error("ParseLevel accepted a level that does not exist")
	}
	if _, err := ParseLevel("  notice "); err != nil {
		t.Errorf("ParseLevel should accept surrounding space and any case: %v", err)
	}
}

// Without the ReplaceAttr every sink installs, slog renders a level it does not
// know as an offset from one it does: NOTICE would reach the sink as "INFO+2".
func TestAddedSeveritiesRenderByName(t *testing.T) {
	for _, testCase := range []struct {
		level Level
		want  string
	}{
		{LevelNotice, "NOTICE"},
		{LevelCritical, "CRITICAL"},
		{LevelAlert, "ALERT"},
		{LevelEmergency, "EMERGENCY"},
	} {
		var out bytes.Buffer
		handler := slog.NewTextHandler(&out, &slog.HandlerOptions{
			Level:       LevelDebug,
			ReplaceAttr: ReplaceLevelWithName,
		})
		newLogger(handler).Log(t.Context(), testCase.level, "checking")

		written := out.String()
		if !strings.Contains(written, "level="+testCase.want) {
			t.Errorf("record at %s rendered as %q, want level=%s",
				testCase.want, strings.TrimSpace(written), testCase.want)
		}
	}
}

func TestLoggerExposesAllEightSeverities(t *testing.T) {
	sink := &recordingSink{level: LevelDebug}
	logger := newLogger(sink)

	logger.Debug("a")
	logger.Info("b")
	logger.Notice("c")
	logger.Warn("d")
	logger.Error("e")
	logger.Critical("f")
	logger.Alert("g")
	logger.Emergency("h")

	want := []Level{
		LevelDebug, LevelInfo, LevelNotice, LevelWarning,
		LevelError, LevelCritical, LevelAlert, LevelEmergency,
	}
	if len(sink.seen) != len(want) {
		t.Fatalf("sink saw %d records, want %d", len(sink.seen), len(want))
	}
	for index, level := range want {
		if sink.seen[index].Level != level {
			t.Errorf("record %d is %s, want %s",
				index, LevelName(sink.seen[index].Level), LevelName(level))
		}
	}
}

// With re-wraps, so a tagged logger keeps the four severities slog lacks.
func TestWithKeepsTheAddedSeverities(t *testing.T) {
	sink := &recordingSink{level: LevelDebug}
	newLogger(sink).With("module", "tcp").Notice("tagged")

	if len(sink.seen) != 1 {
		t.Fatalf("sink saw %d records, want 1", len(sink.seen))
	}
	if got := attrsOf(sink.seen[0])["module"]; got != "tcp" {
		t.Errorf("tagged record carries module=%q, want tcp", got)
	}
}
