package wire

import (
	"crypto/subtle"
	"encoding/base64"
	"strings"
)

func BasicAuthorizationValue(username, password string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(username+":"+password))
}

// BasicCredentialsMatch reports whether an Authorization or
// Proxy-Authorization value carries username and password. The scheme name is
// case-insensitive (RFC 7617); the credentials compare in constant time.
func BasicCredentialsMatch(authorizationValue, username, password string) bool {
	scheme, encodedCredentials, _ := strings.Cut(strings.TrimSpace(authorizationValue), " ")
	if !strings.EqualFold(scheme, "Basic") {
		return false
	}
	expectedCredentials := base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
	return subtle.ConstantTimeCompare([]byte(strings.TrimSpace(encodedCredentials)), []byte(expectedCredentials)) == 1
}
