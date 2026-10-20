// Package controlcharacter finds control characters in the text a user is named
// or logs in with. HTTP Basic forbids them in a user name and a password
// (RFC 7617), and a name is written to logs and API answers, where a newline
// would forge a line. Every other character is allowed, whatever the language:
package controlcharacter

func Contains(text string) bool {
	for _, character := range text {
		if character < 0x20 || character == 0x7f {
			return true
		}
	}
	return false
}
