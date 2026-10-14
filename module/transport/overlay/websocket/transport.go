// Package websocket is the WebSocket overlay transport (RFC 6455, over
// HTTP/1.1): it runs the upgrade handshake over the stream below, then carries
// the payload in binary frames. It exists to pass through CDNs and HTTP
// reverse proxies. Early data is compatible with Xray and sing-box.
package websocket

import (
	"github.com/we-apranik/apranik/config/types"
	"github.com/we-apranik/apranik/spec"
)

// One value per configured component, so nothing has to narrow a config.
type Transport struct{ Settings }

func (*Transport) Info() spec.ModuleInfo {
	return spec.ModuleInfo{Name: "ws", Description: "WebSocket (RFC 6455) over the stream below"}
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

// The return type is the kind declaration: registering this as anything else
// will not compile.
func NewOverlayStreamTransportModule() spec.OverlayStreamTransportModule {
	return &Transport{Settings: Settings{Path: "/", OriginalSourceIP: types.DefaultOriginalSourceIP()}}
}
