package spec

import (
	"context"
	"net"
)

// Transports make and wrap carriers. They know nothing about proxying.

// StreamDialer opens a Stream to dst.
//
// Close releases whatever the dialer holds open on its own account -- a gRPC
// or HTTP/2 overlay keeps a persistent connection multiplexing many Streams,
// and something has to shut it down. A dialer that holds nothing returns nil.
// Closing a dialer does not close Streams it has already handed out.
type StreamDialer interface {
	DialStream(ctx context.Context, dst Destination) (Stream, error)
	Close() error
}

// StreamListener hands out incoming Streams.
// One accepted connection below can produce many Streams here (gRPC, HTTP/2).
type StreamListener interface {
	AcceptStream(ctx context.Context) (Stream, error)
	LocalAddr() net.Addr
	Close() error
}

// DatagramDialer is the sending side of a datagram transport.
// Close: see StreamDialer.
type DatagramDialer interface {
	// CreateDatagram opens an unconnected socket, optionally on localAddr, as
	// the carrier to send through. One carrier reaches any number of
	// destinations, addressed per message in WriteTo and ReadFrom. Zero
	// localAddr means any address, any free port, dual-stack.
	CreateDatagram(ctx context.Context, localAddr Endpoint) (Datagram, error)

	Close() error
}

// DatagramListener listens on one local endpoint. No accept: everyone shares
// it -- unlike a StreamListener, there is no per-client handshake to produce a
// new one.
type DatagramListener interface {
	ListenDatagram(ctx context.Context) (Datagram, error)
	LocalAddr() net.Addr
	Close() error
}

// New* makes a carrier from nothing. Wrap* needs one underneath.
//
// Core composes a transport stack as a fold. The stack lists a base first and
// overlays after it, ordered outward from the wire:
//
//	["tcp", "tls", "ws"]  =>  TCP, then TLS over it, then WebSocket over that
//
// A base makes dialers and listeners. A stream overlay works on one stream at
// a time: core dials or accepts through the base, then hands each overlay in
// turn the stream everything below it produced. Arbitrary depth is permitted
// but is not a goal: the point is that any overlay substitutes for any other.

// BaseStreamTransportModule: TCP, Unix sockets.
//
// NewStreamListener takes bind as a parameter rather than from settings,
// mirroring DialStream(ctx, dst), which makes a listen address on an outbound
// structurally impossible.
//
// Known limit: bind is an Endpoint, so a transport whose endpoint is not a host
// and port -- a Unix socket path, a TUN device name -- cannot express it.
// Deliberately unsolved while v1 ships tcp and udp.
//
// A base transport is the only layer that resolves a domain, and it does so
// through the Resolver it is given, never on its own. A listener
// accepts from addresses and resolves nothing.
type BaseStreamTransportModule interface {
	Config
	Info() ModuleInfo
	NewStreamDialer(resolver Resolver) (StreamDialer, error)
	NewStreamListener(bind Endpoint) (StreamListener, error)
}

// OverlayStreamTransportModule: TLS, WebSocket, HTTPUpgrade. One stream in,
// one stream out. Protocols that carry many streams over one connection
// (HTTP/2, gRPC) are a different shape and will get their own kind.
//
// The New* methods build once, per role, whatever every connection shares:
// certificates, keys, session caches. A setting only one role needs is
// required there, so a server with no certificate fails at startup.
type OverlayStreamTransportModule interface {
	Config
	Info() ModuleInfo
	NewStreamOverlayClient() (StreamOverlayClient, error)
	NewStreamOverlayServer() (StreamOverlayServer, error)
}

// TransportInfoProvider is optional on a stream overlay transport module, and
// on a stream DNS server module, which sits above its TLS layer as an overlay
// sits above another. Unlike Info, it describes this configuration, so it may
// depend on settings. Only the config check reads it.
type TransportInfoProvider interface {
	TransportInfo() TransportInfo
}

// TransportInfo lets the config check find transports that cannot work
// together. Add a field only for a check at least two transports need.
type TransportInfo struct {
	// ALPN names ("http/1.1", "h2"). A transport speaking a protocol lists what
	// it can speak (WebSocket: "http/1.1"); one negotiating ALPN lists what it
	// offers (TLS: its alpn setting). Everything offered must be speakable by
	// the transport directly above.
	ApplicationProtocols []string

	// The far end needs no matching layer: whatever this transport does
	// (splitting, timing, decoy packets) is invisible to the server, so it
	// works toward any destination, as freedom's must.
	NeedsNoServerSide bool
}

// StreamOverlayClient runs this layer's client side over a stream already
// dialed to the proxy server. server is that server's address, as dialed: a
// layer that needs a name (TLS's SNI) takes it from there.
//
// Core calls it once per connection. ctx ends with the request, not with
// the handshake: the layer bounds its own handshake, with a timeout from its
// own settings. On error it leaves underlyingStream open; core closes it. The returned
// stream's Close closes underlyingStream, and its CloseWrite half-closes it.
type StreamOverlayClient interface {
	WrapClientStream(ctx context.Context, underlyingStream Stream, server Destination) (Stream, error)
}

// StreamOverlayServer runs this layer's server side over an accepted stream.
//
// Core calls it once per connection, in that connection's own goroutine, so
// a client that stalls holds up nobody else. ctx ends only when the inbound
// closes: the layer bounds its own handshake, with a timeout from its own
// settings. Close and error rules are StreamOverlayClient's.
type StreamOverlayServer interface {
	WrapServerStream(ctx context.Context, underlyingStream Stream) (Stream, error)
}

// BaseDatagramTransportModule: UDP. Both sides resolve through resolver: a
// listener's datagrams can be written to a domain too.
type BaseDatagramTransportModule interface {
	Config
	Info() ModuleInfo
	NewDatagramDialer(resolver Resolver) (DatagramDialer, error)
	NewDatagramListener(bind Endpoint, resolver Resolver) (DatagramListener, error)
}

// OverlayDatagramTransportModule: DTLS, per-message obfuscation. It still wraps
// dialers and listeners; whether it moves to the per-carrier shape stream
// overlays have is open.
type OverlayDatagramTransportModule interface {
	Config
	Info() ModuleInfo
	WrapDatagramDialer(underlyingDialer DatagramDialer) (DatagramDialer, error)
	WrapDatagramListener(underlyingListener DatagramListener) (DatagramListener, error)
}
