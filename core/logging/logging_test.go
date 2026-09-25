package logging

import (
	"bytes"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// Silence is the default: with no sinks configured nothing is written anywhere,
// so a user on a censored network leaves no trail unless one is asked for.
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
		ReplaceAttr: replaceLevel,
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

// Close has to reach sinks holding an open file, and tolerate sinks holding
// nothing.
func TestCloseReleasesFileSinks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "apranik.log")
	file, err := NewFileSink(path, LevelDebug)
	if err != nil {
		t.Fatalf("File: %v", err)
	}

	system, err := New(Config{Outputs: []Output{HandlerOutput{Handler: file}, HandlerOutput{Handler: &recordingSink{level: LevelDebug}}}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	system.Logger().Info("written before close")

	if err := system.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := system.Close(); err == nil {
		t.Error("closing twice should surface the already-closed file error")
	}

	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the log back: %v", err)
	}
	if !strings.Contains(string(written), "written before close") {
		t.Errorf("log file holds %q, want the record", written)
	}
}

func TestFileSinkAppendsAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "apranik.log")

	for _, message := range []string{"first run", "second run"} {
		sink, err := NewFileSink(path, LevelDebug)
		if err != nil {
			t.Fatalf("File: %v", err)
		}
		system, err := New(Config{Outputs: []Output{HandlerOutput{Handler: sink}}})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		system.Logger().Info(message)
		if err := system.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	}

	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the log back: %v", err)
	}
	for _, message := range []string{"first run", "second run"} {
		if !strings.Contains(string(written), message) {
			t.Errorf("reopening truncated the log: %q is missing from %q", message, written)
		}
	}
}

func TestMemorySinkKeepsTheMostRecentRecords(t *testing.T) {
	sink := NewMemorySink(3, LevelDebug)
	logger := newLogger(sink)

	for _, message := range []string{"one", "two", "three", "four", "five"} {
		logger.Info(message)
	}

	held := sink.Records()
	if len(held) != 3 {
		t.Fatalf("ring holds %d records, want 3", len(held))
	}
	for index, want := range []string{"three", "four", "five"} {
		if held[index].Message != want {
			t.Errorf("record %d is %q, want %q (oldest first)", index, held[index].Message, want)
		}
	}
}

func TestMemorySinkBeforeItWraps(t *testing.T) {
	sink := NewMemorySink(5, LevelDebug)
	newLogger(sink).Info("only one")

	held := sink.Records()
	if len(held) != 1 || held[0].Message != "only one" {
		t.Errorf("ring holds %v, want exactly the one record", held)
	}
}

// Derived handlers share the one ring, so every goroutine's records land in the
// same buffer under the same lock.
func TestMemorySinkIsRaceFreeAcrossDerivedHandlers(t *testing.T) {
	sink := NewMemorySink(64, LevelDebug)
	logger := newLogger(sink)

	var writers sync.WaitGroup
	for worker := range 8 {
		writers.Add(1)
		go func() {
			defer writers.Done()
			tagged := logger.With("worker", worker)
			for range 20 {
				tagged.Info("concurrent")
			}
		}()
	}
	writers.Wait()

	if held := sink.Records(); len(held) != 64 {
		t.Errorf("ring holds %d records, want it full at 64", len(held))
	}
}

func TestMemorySinkKeepsAttrsFromDerivedHandlers(t *testing.T) {
	sink := NewMemorySink(4, LevelDebug)
	newLogger(sink).With("module", "udp").Info("tagged")

	held := sink.Records()
	if len(held) != 1 {
		t.Fatalf("ring holds %d records, want 1", len(held))
	}
	if got := attrsOf(held[0])["module"]; got != "udp" {
		t.Errorf("stored record carries module=%q, want udp", got)
	}
}

type failingOutput struct{}

func (failingOutput) Validate() error { return nil }
func (failingOutput) open(Level) (slog.Handler, error) {
	return nil, errors.New("cannot open")
}

type closeCountingSink struct {
	recordingSink
	closed int
}

func (s *closeCountingSink) Close() error { s.closed++; return nil }

// Outputs are opened by New and nowhere else, and a failure partway through
// releases what was already opened.
func TestNewOpensOutputsAndUnwindsOnFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "apranik.log")
	config := Config{Outputs: []Output{FileOutput{Path: path}}}
	if _, err := os.Stat(path); err == nil {
		t.Fatal("describing a file output must not create the file")
	}

	system, err := New(config)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("New did not open the file output: %v", err)
	}
	system.Close()

	first := &closeCountingSink{recordingSink: recordingSink{level: LevelDebug}}
	_, err = New(Config{Outputs: []Output{HandlerOutput{Handler: first}, failingOutput{}}})
	if err == nil {
		t.Fatal("a failing output was accepted")
	}
	if first.closed != 1 {
		t.Errorf("the output opened before the failure was closed %d times, want 1", first.closed)
	}
}
