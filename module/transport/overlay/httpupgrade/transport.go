// Package httpupgrade is the HTTPUpgrade overlay transport: the HTTP/1.1
// upgrade handshake WebSocket starts with, then the payload as raw bytes, with
// no WebSocket framing. It passes CDNs that relay a WebSocket upgrade without
// checking its frames, at no cost after the handshake. Compatible with Xray's
// and sing-box's transports of the same name.
package httpupgrade

import (
	"github.com/we-apranik/apranik/config/types"
	"github.com/we-apranik/apranik/spec"
)

type Transport struct{ Settings }

func (*Transport) Info() spec.ModuleInfo {
	return spec.ModuleInfo{Name: "httpupgrade", Description: "HTTP/1.1 upgrade, then raw bytes, over the stream below"}
}

// The upgrade request is HTTP/1.1, so a TLS below must not offer anything else.
func (*Transport) TransportInfo() spec.TransportInfo {
	return spec.TransportInfo{ApplicationProtocols: []string{"http/1.1"}}
}

func (t *Transport) NewStreamOverlayClient() (spec.StreamOverlayClient, error) {
	return newClient(&t.Settings), nil
}

func (t *Transport) NewStreamOverlayServer() (spec.StreamOverlayServer, error) {
	return newServer(&t.Settings), nil
}

func NewOverlayStreamTransportModule() spec.OverlayStreamTransportModule {
	return &Transport{Settings: Settings{Path: "/", AcceptWebSocketKey: true, OriginalSourceIP: types.DefaultOriginalSourceIP()}}
}
