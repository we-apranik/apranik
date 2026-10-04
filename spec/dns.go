package spec

import (
	"context"
	"net/netip"
)

// StreamDNSServerModule is a DNS server reached over a stream: DNS over TCP,
// DoT, DoH. It frames one DNS message at a time on streams from dialer, and
// keeps and reuses those streams itself, because only the protocol knows how a
// connection is shared.
//
// Core builds the dialer from the server's outbound, with a TLS layer on top
// when NeedsTLS says so. The module never builds a transport, never sees the
// outbound, and never reads the user's TLS settings.
type StreamDNSServerModule interface {
	Config
	Info() ModuleInfo

	// NeedsTLS: the dialer's streams are encrypted. Core builds the layer from
	// the server's tls field, or from the tls defaults when it is absent, and
	// refuses a tls field on a module that says false.
	NeedsTLS() bool

	NewStreamDNSServer(dialer StreamDialer) (DNSServer, error)
}

// DatagramDNSServerModule is a DNS server reached over datagrams: plain DNS
// over UDP. The dialer's datagram is unconnected: address the resolver in each
// WriteTo.
type DatagramDNSServerModule interface {
	Config
	Info() ModuleInfo
	NewDatagramDNSServer(dialer DatagramDialer) (DNSServer, error)
}

// DNSServer answers one DNS message with one DNS message, both in wire format.
// Core builds the query, reads the response, and decides which queries to send;
// the server only carries them.
//
// Core calls Query from many goroutines at once. Close releases the streams or
// datagrams the server holds; it does not close the dialer, which core owns.
type DNSServer interface {
	Query(ctx context.Context, queryMessage []byte) (responseMessage []byte, err error)
	Close() error
}

// Resolver turns a domain into addresses, best first. It is what the base
// transports receive, so they never know whether DNS, the operating system or
// a hosts file answered.
type Resolver interface {
	ResolveDomain(ctx context.Context, domain string) ([]netip.Addr, error)
}
