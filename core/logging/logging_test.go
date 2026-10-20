package logging

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

func TestNoSinksMeansSilent(t *testing.T) {
	system, err := New(Config{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer system.Close()

	logger := system.Logger()
	if logger.Enabled(t.Context(), LevelEmergency) {
		t.Error("a logger with no sinks reports Emergency as enabled")
	}
	logger.Emergency("nothing should carry this")
}

func TestSingleSinkSkipsTheFanOut(t *testing.T) {
	sink := &recordingSink{level: LevelDebug}
	system, err := New(Config{Outputs: []Output{HandlerOutput{Handler: sink}}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer system.Close()

	system.Logger().Info("direct")
	if len(sink.seen) != 1 {
		t.Fatalf("sink saw %d records, want 1", len(sink.seen))
	}
}

func TestLevelFilters(t *testing.T) {
	var out bytes.Buffer
	handler := slog.NewTextHandler(&out, &slog.HandlerOptions{
		Level:       LevelWarning,
		ReplaceAttr: ReplaceLevelWithName,
	})
	system, err := New(Config{Outputs: []Output{HandlerOutput{Handler: handler}}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer system.Close()

	system.Logger().Notice("below the bar")
	system.Logger().Error("above the bar")

	written := out.String()
	if strings.Contains(written, "below the bar") {
		t.Error("a Notice record passed a Warning-level sink")
	}
	if !strings.Contains(written, "above the bar") {
		t.Error("an Error record was dropped by a Warning-level sink")
	}
}

type failingOutput struct{}

func (failingOutput) Validate() error { return nil }
func (failingOutput) Open(Level) (slog.Handler, error) {
	return nil, errors.New("cannot open")
}

type closeCountingSink struct {
	recordingSink
	closed int
}

func (s *closeCountingSink) Close() error { s.closed++; return nil }

func TestNewOpensOutputsAndUnwindsOnFailure(t *testing.T) {
	first := &closeCountingSink{recordingSink: recordingSink{level: LevelDebug}}
	_, err := New(Config{Outputs: []Output{HandlerOutput{Handler: first}, failingOutput{}}})
	if err == nil {
		t.Fatal("a failing output was accepted")
	}
	if first.closed != 1 {
		t.Errorf("the output opened before the failure was closed %d times, want 1", first.closed)
	}
}
