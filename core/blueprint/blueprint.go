// Package blueprint describes what to build: every inbound and outbound, with
// each module looked up, decoded and validated, but nothing opened. Keeping
// that apart from building lets a config file be checked on a machine that could
// never run it, and lets an embedder write one in Go without JSON.
package blueprint

import (
	"encoding/json"
	"time"

	"github.com/we-apranik/apranik/core/logging"
	"github.com/we-apranik/apranik/core/net/udp/nat"
	"github.com/we-apranik/apranik/spec"
)

const Version = "0.1.0"

const SchemaVersion = 1

type Blueprint struct {
	Version   int
	Log       Log
	Inbounds  []Inbound
	Outbounds []Outbound

	Datasets []Dataset

	DNS *DNS

	Routing *Routing

	Stats *Stats

	APIs []spec.APIModule
}

type Log = logging.Config

type StreamTransportStack struct {
	Base     spec.BaseStreamTransportModule
	Overlays []spec.OverlayStreamTransportModule
}

type DatagramTransportStack struct {
	Base     spec.BaseDatagramTransportModule
	Overlays []spec.OverlayDatagramTransportModule
}

func (s StreamTransportStack) IsZero() bool   { return s.Base == nil }
func (s DatagramTransportStack) IsZero() bool { return s.Base == nil }

type Inbound struct {
	Tag string

	// Required, with no default: an inbound that silently bound every
	// interface because a field was omitted is a real exposure.
	Bind spec.Endpoint

	Network spec.Network

	StreamTransport   StreamTransportStack
	DatagramTransport DatagramTransportStack

	StreamProxy   spec.StreamProxyModule
	DatagramProxy spec.DatagramProxyModule

	Mixed *Mixed

	DatagramOverStreamProxy spec.DatagramOverStreamProxyModule

	Fallback spec.Destination

	MuxCool *MuxCool

	TCPIdleTimeout *time.Duration

	UDPSessionIdleTimeout *time.Duration

	UDPMaxDatagramSize *int

	UDPRoutingCacheEntryIdleTimeout *time.Duration

	UDPRoutingCacheMaxEntriesPerSession *int

	MaxTCPConnections *int

	MaxUDPSessions *int
}

// Exactly one of Proxy, Freedom and Group is set; config.Check refuses anything
// else. Blocking is a routing action, not an outbound.
type Outbound struct {
	Tag string

	Server spec.Destination

	StreamTransport StreamTransportStack

	Proxy spec.StreamProxyModule

	DatagramOverStreamProxy spec.DatagramOverStreamProxyModule

	Freedom *Freedom

	Group *Group

	MuxCool *MuxCool

	ProxyRequestTiming *ProxyRequestTiming

	// Only with Proxy: the DNS server that resolves Server when it is a
	// domain, so the proxy's own name never loops through a DNS server that
	// uses the proxy. Empty means the dns section's default server.
	DNSServer string
}

type Mixed struct {
	SOCKS5 spec.StreamProxyModule
	HTTP   spec.StreamProxyModule

	Users []json.RawMessage

	FirstByteTimeout *time.Duration
}

type Freedom struct {
	StreamTransport   StreamTransportStack
	DatagramTransport DatagramTransportStack

	NAT *nat.Policy

	NATMaxDatagramSize *int
}
