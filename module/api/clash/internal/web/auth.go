package web

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"strings"
)

// Authenticate answers 401 to a request without the secret. A WebSocket
// handshake cannot set headers from a browser, so only an upgrade may carry the
// secret as ?token=: a query string is logged and kept in history, and the
// header is not.
func Authenticate(secret string, next http.Handler) http.Handler {
	expected := sha256.Sum256([]byte(secret))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		presented, found := bearerToken(r)
		if !found && IsWebSocketUpgrade(r) {
			presented = r.URL.Query().Get("token")
		}
		// Digests of one length, so the comparison does not depend on how much of
		// the secret was right.
		presentedDigest := sha256.Sum256([]byte(presented))
		if presented == "" || subtle.ConstantTimeCompare(presentedDigest[:], expected[:]) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="apranik"`)
			WriteError(w, http.StatusUnauthorized, "Unauthorized")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func bearerToken(r *http.Request) (token string, found bool) {
	scheme, credentials, ok := strings.Cut(r.Header.Get("Authorization"), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	return strings.TrimSpace(credentials), true
}

func IsWebSocketUpgrade(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket") && headerHasToken(r.Header, "Connection", "upgrade")
}

func headerHasToken(header http.Header, name, token string) bool {
	for _, value := range header.Values(name) {
		for _, part := range strings.Split(value, ",") {
			if strings.EqualFold(strings.TrimSpace(part), token) {
				return true
			}
		}
	}
	return false
}
