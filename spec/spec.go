// Package spec is the contract every Apranik module implements. So that a new
// technique can ship as one module without touching anything else, every
// interface lives here and spec imports nothing from the project: modules
// import spec, core imports modules, and nothing points back.
//
// # Modules
//
// A module is a transport, a proxy protocol or a sniffer, added without
// touching core dispatch. In Go it is a package.
//
// It contributes one value per kind it fills, and that value both carries the
// user's settings and builds the working component from them, the way
// net.Dialer is both a set of options and the thing that dials. The kind
// interfaces below are what such a value implements.
//
// Each module exports one package-level constructor per kind:
//
//	// transport/tcp
//	func NewBaseStreamTransportModule() spec.BaseStreamTransportModule
//
//	// proxy/ss2022 -- one module, three kinds
//	func NewStreamProxyModule() spec.StreamProxyModule
//	func NewDatagramOverStreamProxyModule() spec.DatagramOverStreamProxyModule
//	func NewDatagramProxyModule() spec.DatagramProxyModule
//
//	// sniffer/tls
//	func NewSnifferModule() spec.SnifferModule
//
// The return type is the kind declaration, so registering a module under the
// wrong kind does not compile and nothing here needs a type assertion.
package spec

import (
	"context"
	"errors"
	"io"
	"net"
	"net/netip"
	"strconv"
	"time"
)

// Network is what the traffic is, not what the wire is.
// UDP traffic often travels over a TCP carrier.
type Network string

const (
	NetworkTCP Network = "tcp"
	NetworkUDP Network = "udp"
)

// Endpoint is one concrete address: an IP and a port, already resolved. Local
// addresses to bind, a peer a packet arrived from, an address reported back
// over the wire.
//
// The fields are unexported for the same reason Destination's are: the IP is
// stored unmapped, so a 4-in-6 spelling of an IPv4 address cannot reach a
// caller that switches on Is4 and pick the wrong socket family. Comparable,
// and a zero one is told apart by IsValid.
type Endpoint struct {
	addr netip.Addr
	port uint16
}

func NewEndpoint(ip netip.Addr, port uint16) (Endpoint, error) {
	if !ip.IsValid() {
		return Endpoint{}, errors.New("spec: endpoint has no IP")
	}
	return Endpoint{addr: ip.Unmap(), port: port}, nil
}

// NewEndpointFromNetAddr reads whatever a carrier reports as its peer. A
// carrier with no host-and-port address -- a pipe, a TUN device -- yields a
// zero Endpoint rather than an error, since having no peer address is normal
// for those and not a failure.
//
// TCP and UDP addresses, which is nearly every call, are read directly; any
// other kind goes through its string form.
func NewEndpointFromNetAddr(address net.Addr) Endpoint {
	var addressAndPort netip.AddrPort
	switch typed := address.(type) {
	case nil:
		return Endpoint{}
	case *net.TCPAddr:
		if typed == nil {
			return Endpoint{}
		}
		addressAndPort = typed.AddrPort()
	case *net.UDPAddr:
		if typed == nil {
			return Endpoint{}
		}
		addressAndPort = typed.AddrPort()
	}
	if addressAndPort.IsValid() {
		return Endpoint{addr: addressAndPort.Addr().Unmap(), port: addressAndPort.Port()}
	}

	host, port, err := net.SplitHostPort(address.String())
	if err != nil {
		return Endpoint{}
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return Endpoint{}
	}
	number, err := strconv.ParseUint(port, 10, 16)
	if err != nil {
		return Endpoint{}
	}
	return Endpoint{addr: ip.Unmap(), port: uint16(number)}
}

func (e Endpoint) Addr() netip.Addr { return e.addr }

func (e Endpoint) Port() uint16 { return e.port }

func (e Endpoint) IsValid() bool { return e.addr.IsValid() }

func (e Endpoint) String() string {
	if !e.IsValid() {
		return "<none>"
	}
	return netip.AddrPortFrom(e.addr, e.port).String()
}

func (e Endpoint) Destination() Destination {
	return Destination{ip: e.addr, port: e.port}
}

// Destination is where traffic is headed: exactly one of a domain or an IP,
// with a port. A domain stays unresolved, because only the remote side should
// resolve it -- resolving here would leak the lookup to the local network.
//
// The fields are unexported so that invariant holds by construction: neither
// "both" nor "neither" can be built, and no layer downstream has to re-check.
// Comparable.
type Destination struct {
	domain string
	ip     netip.Addr
	port   uint16
}

func NewDestinationFromDomain(domain string, port uint16) (Destination, error) {
	if domain == "" {
		return Destination{}, errors.New("spec: destination has an empty domain")
	}
	return Destination{domain: domain, port: port}, nil
}

func NewDestinationFromIP(ip netip.Addr, port uint16) (Destination, error) {
	if !ip.IsValid() {
		return Destination{}, errors.New("spec: destination has no IP")
	}
	return Destination{ip: ip.Unmap(), port: port}, nil
}

// Domain reports the domain, and whether this destination is one at all.
func (d Destination) Domain() (string, bool) { return d.domain, d.domain != "" }

func (d Destination) IP() (netip.Addr, bool) { return d.ip, d.ip.IsValid() }

func (d Destination) Port() uint16 { return d.port }

func (d Destination) IsValid() bool { return d.domain != "" || d.ip.IsValid() }

// Host is the domain or the IP as text, ready for net.JoinHostPort. Empty only
// for a zero Destination.
func (d Destination) Host() string {
	if d.domain != "" {
		return d.domain
	}
	if d.ip.IsValid() {
		return d.ip.String()
	}
	return ""
}

func (d Destination) String() string {
	if !d.IsValid() {
		return "<none>"
	}
	return net.JoinHostPort(d.Host(), strconv.Itoa(int(d.port)))
}

// Stream is ordered, reliable bytes. net.Conn plus CloseWrite.
type Stream interface {
	io.ReadWriteCloser

	// CloseWrite says "I'm done sending" without closing the read side.
	// Carriers that can't express this return ErrNoHalfClose.
	CloseWrite() error

	LocalAddr() net.Addr
	RemoteAddr() net.Addr
	SetDeadline(t time.Time) error
	SetReadDeadline(t time.Time) error
	SetWriteDeadline(t time.Time) error
}

// PeekableStream is a Stream whose next bytes can be looked at without
// consuming them. Core implements it; modules receive it.
type PeekableStream interface {
	Stream

	// Peek returns the next n bytes without consuming them; fewer only with an
	// error. It blocks until they arrive or the read deadline passes, may read
	// ahead of n, and refuses an n larger than the implementation's buffer.
	Peek(n int) ([]byte, error)

	// PeekExactly is Peek that never reads past n, so once those bytes are
	// read nothing stays buffered and the underlying stream can be handed on.
	PeekExactly(n int) ([]byte, error)
}

// Datagram is messages with boundaries. Unordered, unreliable.
type Datagram interface {
	ReadFrom(p []byte) (n int, from Destination, err error)
	WriteTo(p []byte, to Destination) (n int, err error)
	Close() error
	LocalAddr() net.Addr
}

// ResolvedWriter is an optional capability on a Datagram. A carrier that
// resolves a domain itself reports the concrete address the write went to, so
// a layer above can match a reply -- which always arrives from an IP -- against
// what was actually sent to. Carriers that never resolve do not implement it,
// and a caller must work without it.
//
// Discovered by type assertion rather than added to Datagram: only a base
// datagram transport can answer it, and widening Datagram would make every
// other implementation carry a method it cannot honour.
type ResolvedWriter interface {
	WriteToResolved(p []byte, to Destination) (n int, sentTo Endpoint, err error)
}

var (
	ErrNoHalfClose             = errors.New("spec: carrier has no half-close")
	ErrMessageTooLong          = errors.New("spec: message too large for carrier")
	ErrUnsupportedNetwork      = errors.New("spec: network not supported here")
	ErrUDPAssociationRequested = errors.New("spec: hand off to a UDP associate handler")
	ErrSniffNeedMoreData       = errors.New("spec: sniffer needs more data")

	// ErrFallbackRequested is returned by Accept* for a request that is not
	// valid for this protocol. Core replays every byte the handshake read, and
	// the rest of the stream, to the inbound's fallback. Return it only before
	// writing anything to the carrier.
	ErrFallbackRequested = errors.New("spec: not this protocol's request; hand the stream to the fallback")
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

// ModuleInfo identifies the module, not the configured component: Info must
// return the same value for every value the constructor produces, independent
// of any setting. The registry checks this by constructing twice.
type ModuleInfo struct {
	Name        string // registry key: "tcp", "tls", "vless", "socks5"
	Description string // one line, for CLI listings and generated docs
}

// Config is what the configuration system decodes into and validates. The kind
// interfaces embed it, so the value the user configures and the component it
// builds are one thing. A module wanting its schema as a separate type embeds
// one: type Transport struct{ Settings }.
//
// Validate runs after decoding and before anything is built, so a bad
// configuration is refused at startup rather than on the first packet. It must
// not dial, listen, read files, or otherwise do work.
//
// Warnings reports advisory findings -- a setting that weakens security, a
// combination that will work but probably is not intended. It runs only when
// Validate returned nil, must not fail, and must not repeat what Validate
// already refused.
//
// Three rules the compiler cannot enforce:
//
//   - Do not put credential values in Validate's or Warnings' text. Use
//     types.Secret for passwords, UUIDs and keys, so %q and %v redact them.
//   - Treat the value as inert once Validate returns. Everything a New* or
//     Wrap* result needs must be derived from the settings alone and shared
//     with nothing else, because a module registered under several kinds is
//     constructed and decoded once per kind.
//   - Do not populate a map field with defaults. encoding/json MERGES into an
//     existing map rather than replacing it, so a default entry survives
//     decoding and the user has no way to remove it. Slices are safe: they are
//     replaced. Verified on Go 1.27.1.
type Config interface {
	Validate() error
	Warnings() []string
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
type BaseStreamTransportModule interface {
	Config
	Info() ModuleInfo
	NewStreamDialer() (StreamDialer, error)
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

// TransportInfoProvider is optional on a stream overlay transport module. Unlike
// Info, it describes this configuration, so it may depend on settings. Only the
// config check reads it.
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
// Core calls it once per connection, bounded by the outbound's dial timeout.
// On error it leaves underlyingStream open; core closes it. The returned
// stream's Close closes underlyingStream, and its CloseWrite half-closes it.
type StreamOverlayClient interface {
	WrapClientStream(ctx context.Context, underlyingStream Stream, server Destination) (Stream, error)
}

// StreamOverlayServer runs this layer's server side over an accepted stream.
//
// Core calls it once per connection, in that connection's own goroutine and
// bounded by the inbound's handshake timeout, so a client that stalls holds
// up nobody else. Close and error rules are StreamOverlayClient's.
type StreamOverlayServer interface {
	WrapServerStream(ctx context.Context, underlyingStream Stream) (Stream, error)
}

// BaseDatagramTransportModule: UDP.
type BaseDatagramTransportModule interface {
	Config
	Info() ModuleInfo
	NewDatagramDialer() (DatagramDialer, error)
	NewDatagramListener(bind Endpoint) (DatagramListener, error)
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

// Metadata is what the client asked for. It is the only thing that crosses
// the proxy layer. Below the proxy layer, addresses mean the proxy server;
// above it, they mean the real destination.
type Metadata struct {
	Network     Network
	Destination Destination // may be zero for UDP: it varies per packet
	Source      Endpoint
	InboundTag  string
	User        string       // set only if the proxy protocol authenticates
	Sniffed     *SniffResult // filled by core after handshake, never by a module
}

type SniffResult struct {
	Protocol string // "tls", "http", "quic", "bittorrent"
	Domain   string // empty when the protocol carried none
	Port     uint16 // zero when the protocol carried none
}

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

// SnifferModule is a sniffer's settings. NewSniffer builds the Sniffer core
// runs, so anything a sniffer prepares once -- keys, tables, compiled
// patterns -- is prepared there, not in Validate or on the first Sniff.
type SnifferModule interface {
	Config
	Info() ModuleInfo
	NewSniffer() (Sniffer, error)
}

// Sniffer recognises a protocol in the start of a client's payload. It only
// reads: core peeks the bytes without consuming them, and core, not the
// sniffer, fills Metadata.Sniffed. ErrSniffNeedMoreData means the bytes so far
// are a prefix of this protocol; any other error means they are not it.
//
// Core calls Sniff from many connections at once.
type Sniffer interface {
	Sniff(payloadPrefix []byte) (SniffResult, error)
}

// UDPAssociateSocketFunc opens the second socket SOCKS5 UDP ASSOCIATE needs:
// handshake arrives on TCP, but payload needs a separate UDP socket whose
// address is reported back over that TCP. Granted to an inbound's proxy
// server, by core, at construction.
type UDPAssociateSocketFunc func(ctx context.Context) (Datagram, error)

// UDPAssociateHandler is the opt-in hook for a stream proxy server that binds
// a datagram socket mid-handshake -- SOCKS5 UDP ASSOCIATE. Core discovers it by
// type assertion on the StreamProxyServer; an implementation should assert it
// at compile time, so a change here breaks the build rather than UDP.
//
// AcceptStream cannot return a Datagram, so a server signals the handoff by
// returning ErrUDPAssociationRequested. Core then grants the socket function
// once, at construction, through SetAssociateSocket, and calls Associate on
// the same control stream. Associate must open the socket, reply and return
// rather than block for the session's lifetime; core owns the session from
// there, and ends it when the control stream closes.
type UDPAssociateHandler interface {
	SetAssociateSocket(socket UDPAssociateSocketFunc)
	Associate(ctx context.Context, controlStream Stream, metadata Metadata) (Datagram, error)
}
