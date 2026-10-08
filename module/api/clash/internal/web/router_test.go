package web_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/The-Apranik/Apranik/module/api/clash/internal/web"
)

func call(handler http.Handler, method, path string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(method, path, nil))
	return recorder
}

func TestARouterSendsEachMethodAndPatternToItsHandler(t *testing.T) {
	router := web.NewRouter()
	router.Handle("/things/{name}", web.Methods{
		"GET": func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("get " + r.PathValue("name"))) },
		"PUT": func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("put " + r.PathValue("name"))) },
	})
	router.Handle("/{$}", web.Methods{"GET": func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("root")) }})

	for _, test := range []struct{ method, path, want string }{
		{"GET", "/things/a", "get a"},
		{"PUT", "/things/b", "put b"},
		{"GET", "/things/a%20b%2Fc", "get a b/c"},
		{"GET", "/", "root"},
	} {
		if got := call(router, test.method, test.path).Body.String(); got != test.want {
			t.Errorf("%s %s = %q, want %q", test.method, test.path, got, test.want)
		}
	}
}

func TestAnUnknownPathIsAJSON404(t *testing.T) {
	recorder := call(web.NewRouter(), "GET", "/no/such/path")
	if recorder.Code != http.StatusNotFound || recorder.Header().Get("Content-Type") != "application/json" || !strings.Contains(recorder.Body.String(), "nothing at /no/such/path") {
		t.Errorf("%d %q %v", recorder.Code, recorder.Body.String(), recorder.Header())
	}
}

func TestAMethodThePathDoesNotTakeIsAJSON405ThatNamesTheOnesItDoes(t *testing.T) {
	router := web.NewRouter()
	router.Handle("/x", web.Methods{"PUT": func(http.ResponseWriter, *http.Request) {}, "GET": func(http.ResponseWriter, *http.Request) {}})
	recorder := call(router, "DELETE", "/x")
	if recorder.Code != http.StatusMethodNotAllowed || recorder.Header().Get("Allow") != "GET, PUT" || !strings.Contains(recorder.Body.String(), `"message"`) {
		t.Errorf("%d %q Allow=%q", recorder.Code, recorder.Body.String(), recorder.Header().Get("Allow"))
	}
}
