package wire

import (
	"bytes"
	"encoding/base64"
	"strings"
)

func BasicAuthorizationValue(username, password string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(username+":"+password))
}

// BasicCredentials decodes an Authorization or Proxy-Authorization value into
// the username and password it carries. The scheme name is case-insensitive
// (RFC 7617). The username ends at the first colon, so a password may contain
// one and a username may not. False for another scheme or a value that is not
// base64 of "username:password".
func BasicCredentials(authorizationValue string) (username, password []byte, ok bool) {
	scheme, encodedCredentials, _ := strings.Cut(strings.TrimSpace(authorizationValue), " ")
	if !strings.EqualFold(scheme, "Basic") {
		return nil, nil, false
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encodedCredentials))
	if err != nil {
		return nil, nil, false
	}
	username, password, found := bytes.Cut(decoded, []byte(":"))
	if !found {
		return nil, nil, false
	}
	return username, password, true
}
