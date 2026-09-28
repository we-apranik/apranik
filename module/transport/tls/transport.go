// Package tls is the TLS overlay transport: it runs TLS over whatever stream
// the layer below produced, as a client on outbounds and a server on
// inbounds. Standard library crypto/tls only, so the client's ClientHello is
// Go's own and can be told apart from a browser's.
package tls

import (
	"github.com/The-Apranik/Apranik/spec"
)

// One value per configured component, so nothing has to narrow a config.
type Transport struct{ Settings }

func (*Transport) Info() spec.ModuleInfo {
	return spec.ModuleInfo{Name: "tls", Description: "TLS 1.2 and 1.3 over the stream below"}
}

func (t *Transport) NewStreamOverlayClient() (spec.StreamOverlayClient, error) {
	return newClient(&t.Settings)
}

func (t *Transport) NewStreamOverlayServer() (spec.StreamOverlayServer, error) {
	return newServer(&t.Settings)
}

// The return type is the kind declaration: registering this as anything else
// will not compile.
func NewOverlayStreamTransportModule() spec.OverlayStreamTransportModule {
	return &Transport{Settings: Settings{MinVersion: "1.2", MaxVersion: "1.3"}}
}
