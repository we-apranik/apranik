package spec

import (
	"context"
	"encoding/json"
	"time"
)

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

// DatagramDestinationPlacement says where a datagram-over-stream protocol
// writes the destination of the datagrams it carries.
type DatagramDestinationPlacement int

const (
	// Every datagram names its own destination, so one stream carries a UDP
	// session's datagrams to any number of destinations (Trojan).
	DatagramDestinationInEveryDatagram DatagramDestinationPlacement = iota + 1

	// The destination is named once, in the request that opens the stream, so
	// each destination needs its own stream (plain VLESS, plain VMess).
	DatagramDestinationInStreamRequest
)

type DatagramOverStreamProxyClient interface {
	RequestDatagramOverStream(ctx context.Context, underlyingStream Stream, metadata Metadata) (Datagram, error)

	// Required of every client, with no zero value, so one that does not say
	// does not compile. Core opens one stream per destination for
	// DatagramDestinationInStreamRequest.
	DatagramDestinationIsWritten() DatagramDestinationPlacement
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

// A protocol that fills several kinds is one value that is all of them, and it is
// registered once. These compose the kinds above and add no methods; one is named
// for each combination a protocol has, and another is added when a protocol needs it.

// Trojan, VLESS: TCP proxying, and UDP framed inside the same stream.
type StreamAndDatagramOverStreamProxyModule interface {
	StreamProxyModule
	DatagramOverStreamProxyModule
}

// Shadowsocks 2022.
type StreamDatagramOverStreamAndDatagramProxyModule interface {
	StreamProxyModule
	DatagramOverStreamProxyModule
	DatagramProxyModule
}

// UserManager is optional on a proxy server whose users can change while it
// runs. A user is the JSON object of one element of the module's `users` list,
// so a config file and a running server share one shape and one set of checks.
// Core finds it by type assertion on the server it built; a server that does
// not implement it has the users its settings gave it.
//
// Safe to call while the server accepts connections. A change applies to the
// next handshake; connections already accepted are core's to end.
type UserManager interface {
	// AddUser adds the user and returns its name. ErrUserExists when the name
	// is taken in this server, ErrInvalidUser (wrapped, saying what is wrong,
	// never a credential) when the object is not a user this protocol accepts.
	AddUser(user json.RawMessage) (name string, err error)

	// RemoveUser removes the user. ErrNoSuchUser when no user has the name.
	RemoveUser(name string) error

	// ListUsers returns the names, in the order the users were added. Credentials
	// are never returned.
	ListUsers() []string
}

// MuxCoolOptions is what core read from an outbound's muxcool key and hands to
// its protocol: how the protocol sends UDP.
type MuxCoolOptions struct {
	// XUDP sends a UDP session as XUDP: one stream to the server carries every
	// destination of the session. False sends plain protocol UDP.
	XUDP bool
}

// MuxCoolConfigurable is optional on a proxy module that can carry Mux.Cool.
// Core finds it by assertion on the module, so a muxcool key on a protocol
// without it is refused before anything is built, and calls it once with the
// resolved options before it builds the module's clients.
type MuxCoolConfigurable interface {
	ConfigureMuxCool(options MuxCoolOptions) error
}

// ProxyRequestTimingOptions is what core read from an outbound's
// proxy_request_timing key and hands to its protocol: when the client sends the
// request it owes its server.
type ProxyRequestTimingOptions struct {
	// MaxDelay is the longest the client holds its request, waiting for the
	// application's first bytes to travel with it. After that the request goes
	// alone.
	MaxDelay time.Duration

	// SkipPorts are destination ports whose connections send the request at
	// once, without waiting (server-first protocols, which send nothing until
	// the server has answered the request). Empty means every connection waits
	// up to MaxDelay.
	SkipPorts []uint16
}

// ProxyRequestTimingConfigurable is optional on a proxy module whose client
// holds its request until the first payload. Core finds it by assertion on the
// module, so a proxy_request_timing key on a protocol without it is refused
// before anything is built, and calls it once with the resolved options before
// it builds the module's clients.
type ProxyRequestTimingConfigurable interface {
	ConfigureProxyRequestTiming(options ProxyRequestTimingOptions) error
}
