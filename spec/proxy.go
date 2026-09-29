package spec

import "context"

// A proxy protocol is handed a carrier. It never dials.
//
// Accept* = inbound side: read and strip the request preamble, report where
// the client wants to go.
// Request* = outbound side: request a payload from the server over a carrier
// that already reaches it. It connects nothing, and may put nothing on the
// wire until the first write; the server Accepts or refuses the request.
//
// Three shapes, because carrier and payload are independent:
//
//	Stream   -> Stream     normal TCP proxying
//	Stream   -> Datagram   UDP framed inside a TCP-ish carrier (VLESS, Trojan)
//	Datagram -> Datagram   native UDP (Shadowsocks UDP, SOCKS5 associate)

// Note: StreamListener.AcceptStream and StreamProxyServer.AcceptStream share a
// name at different signatures, so no single type can be both. Deliberate --
// a listener is a transport concern, a proxy server is not.

type StreamProxyServer interface {
	// Returns the payload stream with the request framing already consumed.
	AcceptStream(ctx context.Context, underlyingStream Stream) (Stream, Metadata, error)
}

type StreamProxyClient interface {
	// May do nothing on the wire yet: Trojan and VLESS put their request on the
	// first write, so auth failure can surface on the first read instead.
	RequestStream(ctx context.Context, underlyingStream Stream, metadata Metadata) (Stream, error)
}

type DatagramOverStreamProxyServer interface {
	// IsDatagramOverStream peeks at the request and never consumes it. On a
	// stream inbound one handshake can ask for either payload, so core asks
	// this first: true, it calls AcceptDatagramOverStream; false, the same
	// module's AcceptStream. A request it cannot classify is false, left for
	// AcceptStream to refuse. An error means close.
	IsDatagramOverStream(ctx context.Context, underlyingStream PeekableStream) (bool, error)

	// Returns a Datagram whose messages are framed inside c.
	AcceptDatagramOverStream(ctx context.Context, underlyingStream Stream) (Datagram, Metadata, error)
}

type DatagramOverStreamProxyClient interface {
	RequestDatagramOverStream(ctx context.Context, underlyingStream Stream, metadata Metadata) (Datagram, error)
}

type DatagramProxyServer interface {
	// No handshake in the usual sense: often stateless per message.
	// Metadata.Destination may be zero because it varies per packet.
	AcceptDatagram(ctx context.Context, underlyingDatagram Datagram) (Datagram, Metadata, error)
}

type DatagramProxyClient interface {
	RequestDatagram(ctx context.Context, underlyingDatagram Datagram, metadata Metadata) (Datagram, error)
}

// A module fills the kinds its protocol supports and no more. Core checks the
// combination against the configured transport stack while parsing, so a bad
// pairing fails at startup.
//
// VLESS: stream + datagram-over-stream. Shadowsocks 2022: all three. SOCKS5:
// stream only -- its UDP relay is anchored to a stream session, so it can
// never be dispatched from a bare datagram, which is what DatagramProxyModule
// means.

type StreamProxyModule interface {
	Config
	Info() ModuleInfo
	NewStreamServer() (StreamProxyServer, error)
	NewStreamClient() (StreamProxyClient, error)
}

type DatagramOverStreamProxyModule interface {
	Config
	Info() ModuleInfo
	NewDatagramOverStreamServer() (DatagramOverStreamProxyServer, error)
	NewDatagramOverStreamClient() (DatagramOverStreamProxyClient, error)
}

type DatagramProxyModule interface {
	Config
	Info() ModuleInfo
	NewDatagramServer() (DatagramProxyServer, error)
	NewDatagramClient() (DatagramProxyClient, error)
}
