package web_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/The-Apranik/Apranik/module/api/clash/internal/web"
)

const secret = "s3cret"

var protected = web.Authenticate(secret, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("in")) }))

func callWith(handler http.Handler, path string, headers map[string]string) *httptest.ResponseRecorder {
	request := httptest.NewRequest("GET", path, nil)
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func TestARequestWithoutTheSecretIsRefused(t *testing.T) {
	for name, headers := range map[string]map[string]string{
		"no header":              nil,
		"the wrong secret":       {"Authorization": "Bearer nope"},
		"no scheme":              {"Authorization": secret},
		"another scheme":         {"Authorization": "Basic " + secret},
		"an empty token":         {"Authorization": "Bearer "},
		"the secret as a prefix": {"Authorization": "Bearer " + secret + "x"},
	} {
		recorder := callWith(protected, "/", headers)
		if recorder.Code != http.StatusUnauthorized || !strings.Contains(recorder.Body.String(), "Unauthorized") || recorder.Header().Get("WWW-Authenticate") == "" {
			t.Errorf("%s: %d %q, want 401", name, recorder.Code, recorder.Body.String())
		}
	}
}

func TestTheRightSecretIsAcceptedWhateverTheCaseOfTheScheme(t *testing.T) {
	for _, scheme := range []string{"Bearer", "bearer", "BEARER"} {
		if recorder := callWith(protected, "/", map[string]string{"Authorization": scheme + " " + secret}); recorder.Code != http.StatusOK {
			t.Errorf("scheme %q: %d, want 200", scheme, recorder.Code)
		}
	}
}

func TestTheSecretInTheQueryStringIsAcceptedOnlyOnAWebSocketUpgrade(t *testing.T) {
	if recorder := callWith(protected, "/?token="+secret, nil); recorder.Code != http.StatusUnauthorized {
		t.Errorf("a plain request with ?token=: %d, want 401", recorder.Code)
	}
	upgrade := map[string]string{"Upgrade": "websocket", "Connection": "keep-alive, Upgrade"}
	if recorder := callWith(protected, "/?token="+secret, upgrade); recorder.Code != http.StatusOK {
		t.Errorf("an upgrade with ?token=: %d, want 200", recorder.Code)
	}
	if recorder := callWith(protected, "/?token=wrong", upgrade); recorder.Code != http.StatusUnauthorized {
		t.Errorf("an upgrade with the wrong token: %d, want 401", recorder.Code)
	}
}

func TestIsWebSocketUpgradeNeedsBothHeaders(t *testing.T) {
	for headers, want := range map[string]bool{
		"websocket|Upgrade":             true,
		"WebSocket|upgrade":             true,
		"websocket|keep-alive, Upgrade": true,
		"websocket|keep-alive":          false,
		"h2c|Upgrade":                   false,
		"|Upgrade":                      false,
	} {
		upgrade, connection, _ := strings.Cut(headers, "|")
		request := httptest.NewRequest("GET", "/", nil)
		request.Header.Set("Upgrade", upgrade)
		request.Header.Set("Connection", connection)
		if got := web.IsWebSocketUpgrade(request); got != want {
			t.Errorf("Upgrade %q, Connection %q = %v, want %v", upgrade, connection, got, want)
		}
	}
}
