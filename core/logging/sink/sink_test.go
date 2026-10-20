package sink

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/we-apranik/apranik/core/logging"
)

func attrsOf(record slog.Record) map[string]string {
	found := map[string]string{}
	record.Attrs(func(attr slog.Attr) bool {
		found[attr.Key] = attr.Value.String()
		return true
	})
	return found
}

func TestCloseReleasesFileSinks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "apranik.log")
	file, err := NewFile(path, logging.LevelDebug, FormatJSON)
	if err != nil {
		t.Fatalf("File: %v", err)
	}

	manager, err := logging.New(logging.Config{Outputs: []logging.Output{
		logging.HandlerOutput{Handler: file},
		logging.HandlerOutput{Handler: NewMemory(4, logging.LevelDebug)},
	}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	manager.Logger().Info("written before close")

	if err := manager.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := manager.Close(); err == nil {
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

func TestAFileOutputIsOpenedByNewAndNotBefore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "apranik.log")
	config := logging.Config{Outputs: []logging.Output{FileOutput{Path: path}}}
	if _, err := os.Stat(path); err == nil {
		t.Fatal("describing a file output must not create the file")
	}

	manager, err := logging.New(config)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("New did not open the file output: %v", err)
	}
	manager.Close()
}

func TestFileSinkAppendsAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "apranik.log")

	for _, message := range []string{"first run", "second run"} {
		file, err := NewFile(path, logging.LevelDebug, FormatJSON)
		if err != nil {
			t.Fatalf("File: %v", err)
		}
		manager, err := logging.New(logging.Config{Outputs: []logging.Output{logging.HandlerOutput{Handler: file}}})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		manager.Logger().Info(message)
		if err := manager.Close(); err != nil {
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

func TestMemoryKeepsTheMostRecentRecords(t *testing.T) {
	memory := NewMemory(3, logging.LevelDebug)
	logger := slog.New(memory)

	for _, message := range []string{"one", "two", "three", "four", "five"} {
		logger.Info(message)
	}

	held := memory.Records()
	if len(held) != 3 {
		t.Fatalf("ring holds %d records, want 3", len(held))
	}
	for index, want := range []string{"three", "four", "five"} {
		if held[index].Message != want {
			t.Errorf("record %d is %q, want %q (oldest first)", index, held[index].Message, want)
		}
	}
}

func TestMemoryBeforeItWraps(t *testing.T) {
	memory := NewMemory(5, logging.LevelDebug)
	slog.New(memory).Info("only one")

	held := memory.Records()
	if len(held) != 1 || held[0].Message != "only one" {
		t.Errorf("ring holds %v, want exactly the one record", held)
	}
}

// Derived handlers share the one ring, so every goroutine's records land in the
// same buffer under the same lock.
func TestMemoryIsRaceFreeAcrossDerivedHandlers(t *testing.T) {
	memory := NewMemory(64, logging.LevelDebug)
	logger := slog.New(memory)

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

	if held := memory.Records(); len(held) != 64 {
		t.Errorf("ring holds %d records, want it full at 64", len(held))
	}
}

func TestMemoryKeepsAttrsFromDerivedHandlers(t *testing.T) {
	memory := NewMemory(4, logging.LevelDebug)
	slog.New(memory).With("module", "udp").Info("tagged")

	held := memory.Records()
	if len(held) != 1 {
		t.Fatalf("ring holds %d records, want 1", len(held))
	}
	if got := attrsOf(held[0])["module"]; got != "udp" {
		t.Errorf("stored record carries module=%q, want udp", got)
	}
}
