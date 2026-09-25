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
//	func NewBaseStreamTransportModule() spec.BaseStreamTransport
//
//	// proxy/ss2022 -- one module, three kinds
//	func NewStreamProxyModule() spec.StreamProxy
//	func NewDatagramOverStreamProxyModule() spec.DatagramOverStreamProxy
//	func NewDatagramProxyModule() spec.DatagramProxy
//
//	// sniffer/tls
//	func NewSnifferModule() spec.Sniffer
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
func NewEndpointFromNetAddr(address net.Addr) Endpoint {
	if address == nil {
		return Endpoint{}
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
// So the inner passed to a Wrap* method is everything below it, already built.
// A base takes nothing. Arbitrary depth is permitted but is not a goal: the
// point is that any overlay substitutes for any other.

// BaseStreamTransport: TCP, Unix sockets.
//
// NewStreamListener takes bind as a parameter rather than from settings,
// mirroring DialStream(ctx, dst), which makes a listen address on an outbound
// structurally impossible.
//
// Known limit: bind is an Endpoint, so a transport whose endpoint is not a host
// and port -- a Unix socket path, a TUN device name -- cannot express it.
// Deliberately unsolved while v1 ships tcp and udp.
type BaseStreamTransport interface {
	Config
	Info() ModuleInfo
	NewStreamDialer() (StreamDialer, error)
	NewStreamListener(bind Endpoint) (StreamListener, error)
}

// OverlayStreamTransport: TLS, WebSocket, gRPC.
type OverlayStreamTransport interface {
	Config
	Info() ModuleInfo
	WrapStreamDialer(inner StreamDialer) (StreamDialer, error)
	WrapStreamListener(inner StreamListener) (StreamListener, error)
}

// BaseDatagramTransport: UDP.
type BaseDatagramTransport interface {
	Config
	Info() ModuleInfo
	NewDatagramDialer() (DatagramDialer, error)
	NewDatagramListener(bind Endpoint) (DatagramListener, error)
}

// OverlayDatagramTransport: DTLS, per-message obfuscation.
type OverlayDatagramTransport interface {
	Config
	Info() ModuleInfo
	WrapDatagramDialer(inner DatagramDialer) (DatagramDialer, error)
	WrapDatagramListener(inner DatagramListener) (DatagramListener, error)
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
// Connect* = outbound side: write the preamble onto a carrier that already
// reaches the server.
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
	AcceptStream(ctx context.Context, c Stream) (Stream, Metadata, error)
}

type StreamProxyClient interface {
	// May do nothing on the wire yet: VLESS puts its header on the first write,
	// so auth failure can surface on the first read instead.
	ConnectStream(ctx context.Context, c Stream, m Metadata) (Stream, error)
}

type DatagramOverStreamProxyServer interface {
	// Returns a Datagram whose messages are framed inside c.
	AcceptDatagramOverStream(ctx context.Context, c Stream) (Datagram, Metadata, error)
}

type DatagramOverStreamProxyClient interface {
	ConnectDatagramOverStream(ctx context.Context, c Stream, m Metadata) (Datagram, error)
}

type DatagramProxyServer interface {
	// No handshake in the usual sense: often stateless per message.
	// Metadata.Destination may be zero because it varies per packet.
	AcceptDatagram(ctx context.Context, c Datagram) (Datagram, Metadata, error)
}

type DatagramProxyClient interface {
	ConnectDatagram(ctx context.Context, c Datagram, m Metadata) (Datagram, error)
}

// A module fills the kinds its protocol supports and no more. Core checks the
// combination against the configured transport stack while parsing, so a bad
// pairing fails at startup.
//
// VLESS: stream + datagram-over-stream. Shadowsocks 2022: all three. SOCKS5:
// stream only -- its UDP relay is anchored to a stream session, so it can
// never be dispatched from a bare datagram, which is what DatagramProxy means.

type StreamProxy interface {
	Config
	Info() ModuleInfo
	NewStreamServer() (StreamProxyServer, error)
	NewStreamClient() (StreamProxyClient, error)
}

type DatagramOverStreamProxy interface {
	Config
	Info() ModuleInfo
	NewDatagramOverStreamServer() (DatagramOverStreamProxyServer, error)
	NewDatagramOverStreamClient() (DatagramOverStreamProxyClient, error)
}

type DatagramProxy interface {
	Config
	Info() ModuleInfo
	NewDatagramServer() (DatagramProxyServer, error)
	NewDatagramClient() (DatagramProxyClient, error)
}

// Sniffer recognises a protocol in the start of a client's payload. It only
// reads: core peeks the bytes without consuming them, and core, not the
// sniffer, fills Metadata.Sniffed. ErrSniffNeedMoreData means the bytes so far
// are a prefix of this protocol; any other error means they are not it.
type Sniffer interface {
	Config
	Info() ModuleInfo
	Sniff(payloadPrefix []byte) (SniffResult, error)
}

// Router picks the outbound for one request. It holds no state, so the same
// metadata always yields the same answer and a caller may cache it. Streams
// ask once per connection; datagrams ask once per new destination.
type Router interface {
	Route(ctx context.Context, m Metadata) (Outbound, error)
}

// TrafficHandler is core. It is the only thing a module calls back into.
type TrafficHandler interface {
	HandleStream(ctx context.Context, c Stream, m Metadata) error

	// A UDP session has no natural end, so core owns the session table
	// and the idle timeout, not the inbound.
	HandleDatagram(ctx context.Context, c Datagram, m Metadata) error
}

// Inbound = listener stack + proxy server.
// Not Listen: a TUN inbound reads packets off a device and accepts nothing.
type Inbound interface {
	Tag() string
	Activate(ctx context.Context, h TrafficHandler) error // returns once ready
	Close() error
}

// Outbound = proxy client + dialer stack.
//
// Networks reports what this outbound can actually carry, so routing can rule
// it out before a packet arrives rather than discovering it on a failed dial.
// Calling a method for a network Networks does not list returns
// ErrUnsupportedNetwork, but that is an invariant violation, not a path
// routing is expected to take.
//
// Shares a name with StreamDialer.DialStream at a different signature, so no
// type can be both. Accepted: core only ever holds an Outbound as an Outbound.
// An Outbound that must act as an inner dialer needs an adapter holding it as
// a field rather than embedding it.
type Outbound interface {
	Tag() string
	Networks() []Network
	DialStream(ctx context.Context, m Metadata) (Stream, error)
	CreateDatagram(ctx context.Context, m Metadata) (Datagram, error)
	Close() error
}

// UDPAssociateSocketFunc opens the second socket SOCKS5 UDP ASSOCIATE needs:
// handshake arrives on TCP, but payload needs a separate UDP socket whose
// address is reported back over that TCP. Granted to an inbound's proxy
// server, by core, at construction.
type UDPAssociateSocketFunc func(ctx context.Context) (Datagram, error)
