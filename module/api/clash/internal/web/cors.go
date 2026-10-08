package web

import (
	"net/http"
	"strings"
)

// CORS lets a browser page from an allowed origin call the API. An origin not
// listed gets no CORS headers, which makes the browser refuse the answer; a
// request without an Origin header is not a browser's and is untouched. A
// preflight is answered before authentication, since a browser sends it without
// the Authorization header. No origins listed: no headers at all.
func CORS(allowedOrigins []string, next http.Handler) http.Handler {
	if len(allowedOrigins) == 0 {
		return next
	}
	allowAny := false
	allowed := make(map[string]bool, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		if origin == "*" {
			allowAny = true
		}
		allowed[strings.ToLower(origin)] = true
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "" || !(allowAny || allowed[strings.ToLower(origin)]) {
			next.ServeHTTP(w, r)
			return
		}
		header := w.Header()
		header.Add("Vary", "Origin")
		header.Set("Access-Control-Allow-Origin", origin)
		if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
			header.Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE")
			header.Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			header.Set("Access-Control-Max-Age", "300")
			// A page on the internet calling a loopback address needs this.
			if r.Header.Get("Access-Control-Request-Private-Network") == "true" {
				header.Set("Access-Control-Allow-Private-Network", "true")
			}
			WriteNoContent(w)
			return
		}
		next.ServeHTTP(w, r)
	})
}
