package config_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/we-apranik/apranik/config"
)

func TestLoadReadsAFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "apranik.json")
	if err := os.WriteFile(path, []byte(goodDocument), 0o600); err != nil {
		t.Fatal(err)
	}

	configuration, diagnostics := config.LoadFile(path, modules(t))
	requireNoErrors(t, diagnostics)
	if len(configuration.Inbounds) != 1 {
		t.Errorf("got %d inbounds, want 1", len(configuration.Inbounds))
	}
}

func TestLoadFetchesAURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "application/json" {
			t.Errorf("Accept = %q, want application/json", r.Header.Get("Accept"))
		}
		w.Write([]byte(goodDocument))
	}))
	defer server.Close()

	configuration, diagnostics := config.LoadURL(t.Context(), server.URL, modules(t))
	requireNoErrors(t, diagnostics)
	if len(configuration.Inbounds) != 1 {
		t.Errorf("got %d inbounds, want 1", len(configuration.Inbounds))
	}

	// httptest serves plain http, and a config file that crossed the wire in the
	// clear could have been altered by anyone on the path.
	if !find(diagnostics, "", "plain HTTP") {
		t.Errorf("no warning about plain HTTP: %v", diagnostics)
	}
}

func TestLoadRefusesABadStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer server.Close()

	configuration, diagnostics := config.LoadURL(t.Context(), server.URL, modules(t))
	if configuration != nil || !diagnostics.HasErrors() {
		t.Fatalf("a 404 was accepted: %v", diagnostics)
	}
	if !find(diagnostics, "", "404") {
		t.Errorf("the status is not reported: %v", diagnostics)
	}
}

func TestLoadCapsTheDocumentSize(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(strings.Repeat(" ", 5<<20)))
	}))
	defer server.Close()

	_, diagnostics := config.LoadURL(t.Context(), server.URL, modules(t))
	if !find(diagnostics, "", "larger than") {
		t.Errorf("an oversized config file was not refused: %v", diagnostics)
	}
}

// Both loaders run Check, so a caller cannot forget it.
func TestLoadChecks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "apranik.json")
	document := strings.Replace(goodDocument, `"listen": "127.0.0.1",`, "", 1)
	if err := os.WriteFile(path, []byte(document), 0o600); err != nil {
		t.Fatal(err)
	}

	_, diagnostics := config.LoadFile(path, modules(t))
	if !find(diagnostics, "inbounds[0].listen", "no default") {
		t.Errorf("LoadFile did not run Check: %v", diagnostics)
	}
}

// Loading and checking must not touch the machine: a file output is described,
// not opened, until the logging system is built.
func TestLoadOpensNothing(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "apranik.log")
	document := strings.Replace(goodDocument, `{"type": "stderr"}`, `{"type": "file", "path": "`+logPath+`"}`, 1)

	diagnostics, failed := loadAndCheck(t, document)
	if failed {
		t.Fatalf("unexpected errors: %v", diagnostics)
	}
	if _, err := os.Stat(logPath); err == nil {
		t.Error("loading the config file created the log file")
	}
}
