package web

import (
	"net/http"
	"sort"
	"strings"
)

type Methods map[string]http.HandlerFunc

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
