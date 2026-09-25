package logging

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// recordingSink captures what reaches it, so a test can assert a sink saw the
// attrs and not just the message.
type recordingSink struct {
	level    Level
	failWith error
	attrs    []slog.Attr
	seen     []slog.Record
}

func (s *recordingSink) Enabled(_ context.Context, level slog.Level) bool {
	return level >= s.level
}

func (s *recordingSink) Handle(_ context.Context, record slog.Record) error {
	stored := record.Clone()
	stored.AddAttrs(s.attrs...)
	s.seen = append(s.seen, stored)
	return s.failWith
}

func (s *recordingSink) WithAttrs(attrs []slog.Attr) slog.Handler {
	s.attrs = append(s.attrs, attrs...)
	return s
}

func (s *recordingSink) WithGroup(name string) slog.Handler {
	s.attrs = append(s.attrs, slog.String("group", name))
	return s
}

func attrsOf(record slog.Record) map[string]string {
	found := map[string]string{}
	record.Attrs(func(attr slog.Attr) bool {
		found[attr.Key] = attr.Value.String()
		return true
	})
	return found
}

func TestEverySinkSeesTheAttrs(t *testing.T) {
	first, second := &recordingSink{level: LevelDebug}, &recordingSink{level: LevelDebug}
	logger := newLogger(&multiHandler{sinks: []slog.Handler{first, second}})

	logger.Info("relayed", "module", "tcp", "bytes", 42)

	for name, sink := range map[string]*recordingSink{"first": first, "second": second} {
		if len(sink.seen) != 1 {
			t.Fatalf("%s sink saw %d records, want 1", name, len(sink.seen))
		}
		attrs := attrsOf(sink.seen[0])
		if attrs["module"] != "tcp" || attrs["bytes"] != "42" {
			t.Errorf("%s sink got attrs %v, want module=tcp bytes=42", name, attrs)
		}
	}
}

// mutatingSink adds an attr to the record it is handed -- what a handler with
// preformatted attrs does -- and keeps it without copying first.
type mutatingSink struct {
	marker string
	seen   []slog.Record
}

func (s *mutatingSink) Enabled(context.Context, slog.Level) bool { return true }

func (s *mutatingSink) Handle(_ context.Context, record slog.Record) error {
	record.AddAttrs(slog.String("sink", s.marker))
	s.seen = append(s.seen, record)
	return nil
}

func (s *mutatingSink) WithAttrs([]slog.Attr) slog.Handler { return s }
func (s *mutatingSink) WithGroup(string) slog.Handler      { return s }

// This is what a missing Record.Clone in Handle costs. Attrs past the record's
// five inline slots live in a backing slice shared by every copy of the record.
// When that slice has spare capacity, two sinks that both AddAttrs write to the
// same slot, and slog marks the damage with a "!BUG" attribute.
func TestCloneKeepsSinksFromCorruptingEachOther(t *testing.T) {
	first, second := &mutatingSink{marker: "first"}, &mutatingSink{marker: "second"}
	multi := &multiHandler{sinks: []slog.Handler{first, second}}

	// Built in steps on purpose: Record.Add sizes the backing slice exactly,
	// but repeated AddAttrs grows it by doubling, which is what leaves the
	// spare slot two sinks can collide over.
	record := slog.NewRecord(time.Now(), LevelInfo, "relayed", 0)
	record.AddAttrs(slog.Int("a", 1), slog.Int("b", 2), slog.Int("c", 3),
		slog.Int("d", 4), slog.Int("e", 5), slog.Int("f", 6))
	record.AddAttrs(slog.Int("g", 7))
	record.AddAttrs(slog.Int("h", 8))

	if err := multi.Handle(t.Context(), record); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	for _, sink := range []*mutatingSink{first, second} {
		if len(sink.seen) != 1 {
			t.Fatalf("%s sink saw %d records, want 1", sink.marker, len(sink.seen))
		}
		attrs := attrsOf(sink.seen[0])
		if bug, damaged := attrs["!BUG"]; damaged {
			t.Errorf("%s sink got a record slog flagged as shared: %s", sink.marker, bug)
		}
		if attrs["sink"] != sink.marker {
			t.Errorf("%s sink stored sink=%q: the record was shared, not cloned",
				sink.marker, attrs["sink"])
		}
	}
}

func TestEnabledIsTrueWhenAnySinkIs(t *testing.T) {
	quiet, verbose := &recordingSink{level: LevelError}, &recordingSink{level: LevelDebug}
	multi := &multiHandler{sinks: []slog.Handler{quiet, verbose}}

	if !multi.Enabled(context.Background(), LevelDebug) {
		t.Error("Enabled is false at Debug, but one sink accepts Debug")
	}

	onlyQuiet := &multiHandler{sinks: []slog.Handler{quiet}}
	if onlyQuiet.Enabled(context.Background(), LevelDebug) {
		t.Error("Enabled is true at Debug, but the only sink starts at Error")
	}
}

// A sink below the record's level must not be handed the record at all.
func TestSinkBelowLevelIsSkipped(t *testing.T) {
	quiet, verbose := &recordingSink{level: LevelError}, &recordingSink{level: LevelDebug}
	logger := newLogger(&multiHandler{sinks: []slog.Handler{quiet, verbose}})

	logger.Info("routine")

	if len(quiet.seen) != 0 {
		t.Errorf("Error-level sink received %d records, want 0", len(quiet.seen))
	}
	if len(verbose.seen) != 1 {
		t.Errorf("Debug-level sink received %d records, want 1", len(verbose.seen))
	}
}

func TestOneFailingSinkDoesNotStopTheOthers(t *testing.T) {
	broken := &recordingSink{level: LevelDebug, failWith: errors.New("disk full")}
	working := &recordingSink{level: LevelDebug}
	multi := &multiHandler{sinks: []slog.Handler{broken, working}}

	err := multi.Handle(context.Background(), slog.NewRecord(time.Now(), LevelInfo, "still written", 0))
	if err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Errorf("Handle returned %v, want the sink's error", err)
	}
	if len(working.seen) != 1 {
		t.Error("the working sink was skipped because an earlier sink failed")
	}
}

func TestWithAttrsAndWithGroupReachEverySink(t *testing.T) {
	first, second := &recordingSink{level: LevelDebug}, &recordingSink{level: LevelDebug}
	multi := &multiHandler{sinks: []slog.Handler{first, second}}

	multi.WithAttrs([]slog.Attr{slog.String("module", "socks5")})
	multi.WithGroup("conn")

	for name, sink := range map[string]*recordingSink{"first": first, "second": second} {
		if len(sink.attrs) != 2 {
			t.Errorf("%s sink got %d attrs from WithAttrs+WithGroup, want 2", name, len(sink.attrs))
		}
	}
}
