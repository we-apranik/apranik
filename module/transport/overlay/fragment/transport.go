// Package fragment is an overlay transport that cuts the TLS ClientHello, the
// one TLS message sent in plaintext, into pieces: several TLS records, several
// TCP packets, or both, with one cut inside the server name. DPI that does
// not reassemble never sees the name whole; the server reassembles and never
// knows. Everything after the ClientHello passes through untouched.
//
// It goes directly below the layer that sends the ClientHello: under tls in a
// proxy outbound, or alone above tcp under freedom, where the ClientHello is
// the user's own.
package fragment

import (
	"context"
	"errors"

	"github.com/we-apranik/apranik/spec"
)

var errNoServerSide = errors.New("fragment has no server side; it only changes outgoing connections")

type Transport struct{ Settings }

func (*Transport) Info() spec.ModuleInfo {
	return spec.ModuleInfo{Name: "fragment", Description: "cut the TLS ClientHello into records and packets to hide the server name"}
}

func (*Transport) TransportInfo() spec.TransportInfo {
	return spec.TransportInfo{NeedsNoServerSide: true}
}

func (t *Transport) NewStreamOverlayClient() (spec.StreamOverlayClient, error) {
	return &client{settings: t.Settings}, nil
}

func (*Transport) NewStreamOverlayServer() (spec.StreamOverlayServer, error) {
	return nil, errNoServerSide
}

func NewOverlayStreamTransportModule() spec.OverlayStreamTransportModule {
	return &Transport{Settings: defaultSettings()}
}

type client struct {
	settings Settings
}

func (c *client) WrapClientStream(_ context.Context, underlyingStream spec.Stream, _ spec.Destination) (spec.Stream, error) {
	return newFragmentingStream(underlyingStream, &c.settings), nil
}
