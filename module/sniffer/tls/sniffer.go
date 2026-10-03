// Package tls is a sniffer: it recognises a TLS ClientHello in the first
// bytes of a stream and reports the server name (SNI) the client asked for,
// so routing can see a domain where the client only gave an IP, and a guess
// at the browser family that sent it.
package tls

import (
	"slices"
	"strings"

	"github.com/The-Apranik/Apranik/shared/tlsclienthello"
	"github.com/The-Apranik/Apranik/spec"
)

type Settings struct{}

func (*Settings) Validate() error    { return nil }
func (*Settings) Warnings() []string { return nil }

type Sniffer struct{ Settings }

func (*Sniffer) Info() spec.ModuleInfo {
	return spec.ModuleInfo{Name: "tls", Description: "sniff server name (SNI) and browser family from a TLS ClientHello"}
}

// Nothing to prepare: the sniffer reads only the bytes it is given.
func (*Sniffer) NewSniffer() (spec.Sniffer, error) { return clientHelloSniffer{}, nil }

func NewSnifferModule() spec.SnifferModule { return &Sniffer{} }

type clientHelloSniffer struct{}

func (clientHelloSniffer) Sniff(payloadPrefix []byte) (spec.SniffResult, error) {
	clientHello, err := tlsclienthello.Reassemble(payloadPrefix)
	if err != nil {
		return spec.SniffResult{}, err
	}
	serverName, err := tlsclienthello.FindServerName(clientHello)
	if err != nil {
		return spec.SniffResult{}, err
	}
	return spec.SniffResult{
		Protocol:      "tls",
		Domain:        strings.TrimSpace(serverName.Name),
		BrowserFamily: guessBrowserFamily(clientHello),
	}, nil
}

const (
	extensionSessionTicket   = 0x0023
	extensionRecordSizeLimit = 0x001c
)

// The signals, checked against every uTLS preset (browser_family_test.go):
// Chromium and Apple clients send GREASE, Firefox never does; Chromium sends
// session_ticket, Safari does not; Firefox sends record_size_limit, which Go
// and most libraries do not. Anything else, or an old client lacking these,
// is left unguessed rather than guessed wrong.
func guessBrowserFamily(clientHello []byte) string {
	extensionTypes, err := tlsclienthello.ExtensionTypes(clientHello)
	if err != nil {
		return ""
	}
	sendsGREASE := slices.ContainsFunc(extensionTypes, tlsclienthello.IsGREASE)
	switch {
	case sendsGREASE && slices.Contains(extensionTypes, extensionSessionTicket):
		return spec.BrowserFamilyChromium
	case sendsGREASE:
		return spec.BrowserFamilySafari
	case slices.Contains(extensionTypes, extensionRecordSizeLimit):
		return spec.BrowserFamilyFirefox
	}
	return ""
}
