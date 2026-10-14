package sink

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/we-apranik/apranik/core/logging"
)

func TestTerminalSinkColorsTheLevelAndKeepsAttributes(t *testing.T) {
	var written bytes.Buffer
	logger := slog.New(newTerminalSink(&written, logging.LevelDebug, true)).With("inbound", "socks-in").WithGroup("request")

	logger.Log(t.Context(), logging.LevelNotice, "Handshake failed", "error", "timeout")

	line := written.String()
	for _, wanted := range []string{ansiBlue + "NOTICE" + ansiReset, "Handshake failed", "inbound=socks-in", "request.error=timeout"} {
		if !strings.Contains(line, wanted) {
			t.Errorf("line %q lacks %q", line, wanted)
		}
	}
	if strings.Contains(line, "msg=") || strings.Contains(line, "level=") {
		t.Errorf("line %q repeats a built-in attribute", line)
	}
}

func TestTerminalSinkDropsRecordsBelowItsLevel(t *testing.T) {
	var written bytes.Buffer
	logger := slog.New(newTerminalSink(&written, logging.LevelWarning, true))

	logger.Info("Inbound active")

	if written.Len() != 0 {
		t.Errorf("wrote %q below the sink's level", written.String())
	}
}

func TestTerminalSinkWithoutColorIsPlainAndAligned(t *testing.T) {
	var written bytes.Buffer
	logger := slog.New(newTerminalSink(&written, logging.LevelDebug, false))

	logger.Info("Inbound active")
	logger.Warn("Handshake failed")

	lines := strings.Split(strings.TrimSpace(written.String()), "\n")
	if strings.Contains(written.String(), "\x1b") {
		t.Errorf("uncolored output %q holds an escape code", written.String())
	}
	if strings.Index(lines[0], "Inbound") != strings.Index(lines[1], "Handshake") {
		t.Errorf("messages do not line up:\n%s", written.String())
	}
}

func TestStderrOutputRefusesColorWithoutTheHumanFormat(t *testing.T) {
	if err := (StderrOutput{Color: true}).Validate(); err == nil {
		t.Error("color on logfmt was accepted")
	}
	if err := (StderrOutput{Format: FormatHuman, Color: true}).Validate(); err != nil {
		t.Errorf("color on human: %v", err)
	}
	if err := (StderrOutput{Format: "xml"}).Validate(); err == nil {
		t.Error("an unknown format was accepted")
	}
}

func TestFileOutputWritesTheFormatItIsGiven(t *testing.T) {
	for _, test := range []struct {
		format     Format
		wantPrefix string
	}{
		{"", `{"time":`},
		{FormatLogfmt, "time="},
		{FormatHuman, "20"},
	} {
		path := filepath.Join(t.TempDir(), "apranik.log")
		system, err := logging.New(logging.Config{Outputs: []logging.Output{FileOutput{Path: path, Format: test.format}}})
		if err != nil {
			t.Fatalf("format %q: %v", test.format, err)
		}
		system.Logger().Info("Inbound active")
		system.Close()

		written, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(string(written), test.wantPrefix) || strings.Contains(string(written), "\x1b") {
			t.Errorf("format %q wrote %q", test.format, written)
		}
	}
}

func TestNoColorEnvironmentTurnsColorOff(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	sink := NewStderr(logging.LevelInfo, FormatHuman, true).(*terminalSink)
	if sink.color {
		t.Error("NO_COLOR was ignored")
	}
}
