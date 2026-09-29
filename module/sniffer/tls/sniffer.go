// Package tls is a sniffer: it recognises a TLS ClientHello in the first
// bytes of a stream and reports the server name (SNI) the client asked for,
// so routing can see a domain where the client only gave an IP.
package tls

import (
	"strings"

	"github.com/The-Apranik/Apranik/shared/tlsclienthello"
	"github.com/The-Apranik/Apranik/spec"
)

type Settings struct{}

func (*Settings) Validate() error    { return nil }
func (*Settings) Warnings() []string { return nil }

type Sniffer struct{ Settings }

func (*Sniffer) Info() spec.ModuleInfo {
	return spec.ModuleInfo{Name: "tls", Description: "sniff server name (SNI) from a TLS ClientHello"}
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
	return spec.SniffResult{Protocol: "tls", Domain: strings.TrimSpace(serverName.Name)}, nil
}
