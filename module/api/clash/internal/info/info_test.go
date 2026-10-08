package info_test

import (
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/The-Apranik/Apranik/module/api/clash/internal/info"
	"github.com/The-Apranik/Apranik/module/api/clash/internal/stream"
	"github.com/The-Apranik/Apranik/module/api/clash/internal/testkit"
	"github.com/The-Apranik/Apranik/module/api/clash/internal/web"
)

func mount(t *testing.T, provider *testkit.Provider) *testkit.Env {
	return testkit.Mount(t, func(router *web.Router, _ *stream.Hub) { info.Register(router, provider) })
}

func TestHelloVersionAndConfigs(t *testing.T) {
	server := mount(t, &testkit.Provider{Version: "1.2.3", LogLevel: slog.Level(2)})

	var hello map[string]string
	if status := server.GetJSON("/", &hello); status != http.StatusOK || hello["hello"] != "clash" {
		t.Errorf("/ = %d %v", status, hello)
	}

	var version struct {
		Meta    bool   `json:"meta"`
		Version string `json:"version"`
	}
	if status := server.GetJSON("/version", &version); status != http.StatusOK || !version.Meta || version.Version != "Apranik 1.2.3" {
		t.Errorf("/version = %d %+v", status, version)
	}

	var configs struct {
		Mode     string   `json:"mode"`
		ModeList []string `json:"mode-list"`
		LogLevel string   `json:"log-level"`
	}
	if status := server.GetJSON("/configs", &configs); status != http.StatusOK || configs.Mode != "rule" ||
		len(configs.ModeList) != 1 || configs.ModeList[0] != "rule" || configs.LogLevel != "info" {
		t.Errorf("/configs = %d %+v (notice maps to info)", status, configs)
	}
}

func TestTheLogLevelOfConfigsUsesClashNames(t *testing.T) {
	for level, want := range map[slog.Level]string{
		slog.LevelDebug: "debug", slog.LevelInfo: "info", slog.Level(2): "info", slog.LevelWarn: "warning",
		slog.LevelError: "error", slog.Level(10): "error", slog.Level(14): "error",
	} {
		server := mount(t, &testkit.Provider{LogLevel: level})
		var configs struct {
			LogLevel string `json:"log-level"`
		}
		server.GetJSON("/configs", &configs)
		if configs.LogLevel != want {
			t.Errorf("level %d = %q, want %q", level, configs.LogLevel, want)
		}
	}
}

func TestConfigsCannotBeSetAndSaysWhatToDoInstead(t *testing.T) {
	server := mount(t, &testkit.Provider{})
	for _, method := range []string{"PUT", "PATCH"} {
		status, body, header := server.Do(method, "/configs", `{"mode":"global"}`)
		if status != http.StatusMethodNotAllowed || header.Get("Allow") != "GET" || !strings.Contains(testkit.MessageOf(t, body), "/reload") {
			t.Errorf("%s /configs = %d %q Allow=%q", method, status, body, header.Get("Allow"))
		}
	}
}
