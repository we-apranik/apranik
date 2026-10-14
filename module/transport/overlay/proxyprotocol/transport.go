// Package proxyprotocol is the PROXY protocol overlay (HAProxy's, versions 1
// and 2): a load balancer or proxy in front of the server writes one header
// before the client's own bytes, naming the client it accepted. This overlay
// reads it, removes it, and reports that client as the connection's source. It
// is server side only, and it must come directly after the base transport: it
// reads the first bytes of the connection, before any other layer touches
// them. Not to be confused with a proxy protocol such as SOCKS5.
package proxyprotocol

import (
	"errors"

	"github.com/we-apranik/apranik/config/types"
	"github.com/we-apranik/apranik/spec"
)

var errNoClientSide = errors.New("proxy_protocol has no client side; it only reads what a proxy in front of the server sends")

// One value per configured component, so nothing has to narrow a config.
type Transport struct{ Settings }

func (*Transport) Info() spec.ModuleInfo {
	return spec.ModuleInfo{Name: "proxy_protocol", Description: "read the client's address from a PROXY protocol header (HAProxy, nginx, AWS), v1 and v2"}
}

func (*Transport) NewStreamOverlayClient() (spec.StreamOverlayClient, error) {
	return nil, errNoClientSide
}

func (t *Transport) NewStreamOverlayServer() (spec.StreamOverlayServer, error) {
	return newServer(&t.Settings), nil
}

// The return type is the kind declaration: registering this as anything else
// will not compile.
func NewOverlayStreamTransportModule() spec.OverlayStreamTransportModule {
	return &Transport{Settings: Settings{Verification: Verification{TrustedProxyAddresses: loopbackOnly()}}}
}

// Only a proxy on this machine may send the header unless the operator says
// more. A slice, so a config file replaces it instead of merging into it.
func loopbackOnly() types.AddressRanges {
	ranges, err := types.ParseAddressRanges("127.0.0.0/8", "::1")
	if err != nil {
		panic(err)
	}
	return ranges
}
