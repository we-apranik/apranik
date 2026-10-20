package tls

import (
	"slices"
	"strings"

	"github.com/we-apranik/apranik/shared/tlsclienthello"
	"github.com/we-apranik/apranik/spec"
)

type Settings struct{}

func (*Settings) Validate() error    { return nil }
func (*Settings) Warnings() []string { return nil }

type Sniffer struct{ Settings }

func (*Sniffer) Info() spec.ModuleInfo {
	return spec.ModuleInfo{Name: "tls", Description: "sniff server name (SNI) and browser family from a TLS ClientHello"}
}

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
