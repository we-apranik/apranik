// Package web is the HTTP plumbing every Clash endpoint shares: a router that
// answers a missing path or method in JSON, the JSON answers themselves, and
// the authentication and CORS middleware.
package web

import (
	"net/http"
	"sort"
	"strings"
)

// Methods maps an HTTP method to the handler that serves it on one path.
type Methods map[string]http.HandlerFunc

// Router sends a request to the handler for its path and method. A path nobody
// registered is a JSON 404, and a registered path used with another method a
// JSON 405 naming the methods it takes.
type Router struct {
	mux *http.ServeMux
}

func NewRouter() *Router {
	router := &Router{mux: http.NewServeMux()}
	router.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		WriteError(w, http.StatusNotFound, "there is nothing at %s", r.URL.Path)
	})
	return router
}

// Handle registers the handlers for a pattern in net/http's syntax, such as
// "/proxies/{name}/delay".
func (r *Router) Handle(pattern string, methods Methods) {
	allowed := make([]string, 0, len(methods))
	for method := range methods {
		allowed = append(allowed, method)
	}
	sort.Strings(allowed)
	allowedText := strings.Join(allowed, ", ")

	r.mux.HandleFunc(pattern, func(w http.ResponseWriter, request *http.Request) {
		handler, found := methods[request.Method]
		if !found {
			w.Header().Set("Allow", allowedText)
			WriteError(w, http.StatusMethodNotAllowed, "%s is not supported here; this path takes %s", request.Method, allowedText)
			return
		}
		handler(w, request)
	})
}

func (r *Router) ServeHTTP(w http.ResponseWriter, request *http.Request) {
	r.mux.ServeHTTP(w, request)
}
