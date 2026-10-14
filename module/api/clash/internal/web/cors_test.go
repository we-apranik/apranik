package web_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/we-apranik/apranik/module/api/clash/internal/web"
)

func corsHandler(origins ...string) http.Handler {
	return web.CORS(origins, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) }))
}

func TestAnAllowedOriginGetsCORSHeadersOnItsAnswers(t *testing.T) {
	handler := corsHandler("https://dash.example")

	recorder := callWith(handler, "/", map[string]string{"Origin": "https://dash.example"})
	if recorder.Header().Get("Access-Control-Allow-Origin") != "https://dash.example" || recorder.Header().Get("Vary") != "Origin" || recorder.Body.String() != "ok" {
		t.Errorf("headers %v body %q", recorder.Header(), recorder.Body.String())
	}
	if callWith(handler, "/", map[string]string{"Origin": "https://evil.example"}).Header().Get("Access-Control-Allow-Origin") != "" {
		t.Error("an origin that is not allowed got CORS headers")
	}
	if callWith(handler, "/", nil).Header().Get("Access-Control-Allow-Origin") != "" {
		t.Error("a request without an Origin got CORS headers")
	}
}

func TestAPreflightIsAnsweredWithoutPassingItOn(t *testing.T) {
	handler := corsHandler("https://dash.example")
	request := httptest.NewRequest("OPTIONS", "/proxies", nil)
	request.Header.Set("Origin", "https://dash.example")
	request.Header.Set("Access-Control-Request-Method", "PUT")
	request.Header.Set("Access-Control-Request-Private-Network", "true")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	header := recorder.Header()
	if recorder.Code != http.StatusNoContent || recorder.Body.Len() != 0 ||
		header.Get("Access-Control-Allow-Methods") != "GET, POST, PUT, PATCH, DELETE" ||
		header.Get("Access-Control-Allow-Headers") != "Content-Type, Authorization" ||
		header.Get("Access-Control-Allow-Private-Network") != "true" || header.Get("Access-Control-Max-Age") != "300" {
		t.Errorf("%d headers %v", recorder.Code, header)
	}
}

func TestAPreflightFromAnOriginThatIsNotAllowedIsNotAnswered(t *testing.T) {
	request := httptest.NewRequest("OPTIONS", "/proxies", nil)
	request.Header.Set("Origin", "https://evil.example")
	request.Header.Set("Access-Control-Request-Method", "PUT")
	recorder := httptest.NewRecorder()
	corsHandler("https://dash.example").ServeHTTP(recorder, request)
	if recorder.Code == http.StatusNoContent || recorder.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Errorf("%d %v", recorder.Code, recorder.Header())
	}
}

func TestStarAllowsEveryOrigin(t *testing.T) {
	recorder := callWith(corsHandler("*"), "/", map[string]string{"Origin": "https://anything.example"})
	if recorder.Header().Get("Access-Control-Allow-Origin") != "https://anything.example" {
		t.Errorf("headers %v", recorder.Header())
	}
}

func TestWithoutAllowedOriginsThereAreNoCORSHeadersAtAll(t *testing.T) {
	recorder := callWith(corsHandler(), "/", map[string]string{"Origin": "https://dash.example"})
	if recorder.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Errorf("headers %v", recorder.Header())
	}
}
