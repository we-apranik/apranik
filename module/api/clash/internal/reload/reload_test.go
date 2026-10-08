package reload_test

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/The-Apranik/Apranik/module/api/clash/internal/reload"
	"github.com/The-Apranik/Apranik/module/api/clash/internal/stream"
	"github.com/The-Apranik/Apranik/module/api/clash/internal/testkit"
	"github.com/The-Apranik/Apranik/module/api/clash/internal/web"
	"github.com/The-Apranik/Apranik/spec"
)

func mount(t *testing.T, provider *testkit.Provider) *testkit.Env {
	return testkit.Mount(t, func(router *web.Router, _ *stream.Hub) { reload.Register(router, provider) })
}

func TestReloadAnswersByWhatTheCoreSaid(t *testing.T) {
	for name, test := range map[string]struct {
		err    error
		status int
		in     string
	}{
		"accepted":    {nil, http.StatusAccepted, "being loaded"},
		"invalid":     {errors.Join(spec.ErrAPIInvalidConfiguration, errors.New("outbounds[0]: bad")), http.StatusBadRequest, "outbounds[0]: bad"},
		"in progress": {spec.ErrAPIReloadInProgress, http.StatusConflict, "already waiting"},
		"unavailable": {spec.ErrAPIReloadUnavailable, http.StatusNotImplemented, "not available"},
		"other":       {errors.New("boom"), http.StatusInternalServerError, "boom"},
	} {
		server := mount(t, &testkit.Provider{RequestReloadFunc: func() error { return test.err }})
		status, body, _ := server.Do("POST", "/reload", "")
		if status != test.status || !strings.Contains(testkit.MessageOf(t, body), test.in) {
			t.Errorf("%s: %d %q, want %d containing %q", name, status, body, test.status, test.in)
		}
	}
}

func TestReloadIsAPostOnly(t *testing.T) {
	server := mount(t, &testkit.Provider{})
	if status, _, header := server.Do("GET", "/reload", ""); status != http.StatusMethodNotAllowed || header.Get("Allow") != "POST" {
		t.Errorf("GET /reload = %d Allow=%q", status, header.Get("Allow"))
	}
}
