package ui_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/we-apranik/apranik/module/api/clash/internal/ui"
)

func serve(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	directory := t.TempDir()
	os.WriteFile(filepath.Join(directory, "index.html"), []byte("<h1>dashboard</h1>"), 0o644)
	os.Mkdir(filepath.Join(directory, "assets"), 0o755)
	os.WriteFile(filepath.Join(directory, "assets", "app.js"), []byte("console.log(1)"), 0o644)
	files, err := ui.Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { files.Close() })
	server := httptest.NewServer(files)
	t.Cleanup(server.Close)
	return server, directory
}

func get(t *testing.T, server *httptest.Server, method, path string) (int, string, http.Header) {
	t.Helper()
	request, _ := http.NewRequest(method, server.URL+path, nil)
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	return response.StatusCode, string(body), response.Header
}

func TestServesTheIndexAndTheFilesBelowIt(t *testing.T) {
	server, _ := serve(t)
	if status, body, _ := get(t, server, "GET", "/ui/"); status != 200 || !strings.Contains(body, "dashboard") {
		t.Errorf("index: %d %q", status, body)
	}
	if status, body, _ := get(t, server, "GET", "/ui/assets/app.js"); status != 200 || body != "console.log(1)" {
		t.Errorf("file: %d %q", status, body)
	}
	if status, _, _ := get(t, server, "GET", "/ui/missing.js"); status != 404 {
		t.Errorf("missing: %d", status)
	}
}

func TestUIWithoutTheSlashRedirectsToIt(t *testing.T) {
	server, _ := serve(t)
	status, _, header := get(t, server, "GET", "/ui")
	if status != http.StatusMovedPermanently || header.Get("Location") != "/ui/" {
		t.Errorf("%d to %q", status, header.Get("Location"))
	}
}

func TestOnlyReadingIsAllowed(t *testing.T) {
	server, _ := serve(t)
	status, _, header := get(t, server, "DELETE", "/ui/index.html")
	if status != http.StatusMethodNotAllowed || header.Get("Allow") == "" {
		t.Errorf("%d", status)
	}
}

func TestASymbolicLinkCannotLeadOutOfTheDirectory(t *testing.T) {
	server, directory := serve(t)
	outside := filepath.Join(t.TempDir(), "secret.txt")
	os.WriteFile(outside, []byte("private"), 0o644)
	if err := os.Symlink(outside, filepath.Join(directory, "leak.txt")); err != nil {
		t.Skip("symbolic links are not available:", err)
	}
	if status, body, _ := get(t, server, "GET", "/ui/leak.txt"); status == 200 || strings.Contains(body, "private") {
		t.Errorf("%d %q left the directory", status, body)
	}
	if status, _, _ := get(t, server, "GET", "/ui/../secret.txt"); status == 200 {
		t.Errorf("dot-dot answered %d", status)
	}
}

func TestOpenRefusesAMissingDirectory(t *testing.T) {
	if _, err := ui.Open(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Error("a missing directory was opened")
	}
}

func TestServesTellsDashboardPathsApart(t *testing.T) {
	for path, want := range map[string]bool{"/ui": true, "/ui/": true, "/ui/a/b": true, "/uix": false, "/": false, "/proxies": false} {
		if ui.Serves(path) != want {
			t.Errorf("Serves(%q) = %v", path, !want)
		}
	}
}
