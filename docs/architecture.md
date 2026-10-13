# Architecture

Apranik is a few layers. Each one imports only the layers below it, and the
bottom layer, `spec`, imports nothing from the project.

[![Layers and import direction: cmd/apranik imports config, config imports core, core's registry names the modules, modules may import shared helper packages, and everything implements or uses spec. Every layer imports spec directly; spec imports nothing from the project. Modules also use config/types for shared JSON values.](assets/diagrams/architecture-overview.excalidraw.svg)](assets/diagrams/architecture-overview.excalidraw.svg)


| Layer | Holds | May import |
| --- | --- | --- |
| `spec` | the module contract: interfaces, the address types (with their constructors) and the sentinel errors; no state | nothing from the project |
| `shared/*` | helper code two or more modules need that is not contract, one concept per package | `spec` and the standard library |
| `module/*` | transports, proxy protocols, sniffers, DNS servers and APIs, one folder each | `spec`, `shared/*`, and `config/types` for shared JSON values; never another module |
| `core/*` | registry, building, routing, relaying, UDP sessions and NAT, logging; `core/component` declares the roles core's parts play for each other | `spec`, `shared/*` and modules |
| `config` | the JSON config file → `blueprint.Blueprint` | `spec`, `core` |
| `cmd/apranik` | the standalone command | everything above |

## The spec contract

[![What spec declares: two carriers (Stream, Datagram) plus PeekableStream; two address types (Endpoint, Destination); four transport module kinds, with stream overlays wrapping one connection at a time; three proxy module kinds; SnifferModule and Sniffer; what crosses the proxy layer (Metadata, the UDP ASSOCIATE hook, the fallback sentinel); the Config and Info methods every module kind has; and the sentinel errors. Router, TrafficHandler, Inbound and Outbound are not in spec but in core/component.](assets/diagrams/spec-contract.excalidraw.svg)](assets/diagrams/spec-contract.excalidraw.svg)

[`spec/`](https://github.com/The-Apranik/Apranik/tree/main/spec) is the
module contract, one package split into a file per group (`doc.go` lists
them). It holds interfaces, sentinel errors and value types with their
constructors, and no state. Its main groups:

**Carriers.** There are two, and only two:

- `Stream`: `net.Conn` plus `CloseWrite() error`. Half-close is part of the
  contract; a carrier that cannot signal it returns `ErrNoHalfClose` instead of
  leaving a caller hanging.
- `Datagram`: `ReadFrom` / `WriteTo` with message boundaries kept, addressed by
  `Destination`.

`PeekableStream` adds `Peek` and `PeekExactly` to a `Stream`; core implements it
(`core/net/peekable`) and hands it to protocols that need to look before they
read. `ResolvedWriter` is an optional capability of a `Datagram` that resolves
domains itself.

Substrates with no server initiative, such as DNS tunnels or mailbox relays, are
expected to build a `Stream` internally (Decided; none exists yet).

**Addresses.** `Endpoint` is an IP and a port, already resolved. `Destination` is
exactly one of a domain or an IP, plus a port. A domain stays a domain until the
base transport, which is the only layer that resolves, through the
`spec.Resolver` core hands it: `core/dns`, which picks a DNS server by rule and
caches answers.

**Transport kinds.** A base transport creates connections; an overlay wraps them.
A stream overlay works on one stream at a time: it builds a client and a server
once, and each wraps one connection:

| Kind | Methods |
| --- | --- |
| `BaseStreamTransportModule` | `NewStreamDialer`, `NewStreamListener` |
| `OverlayStreamTransportModule` | `NewStreamOverlayClient` → `WrapClientStream`, `NewStreamOverlayServer` → `WrapServerStream` |
| `BaseDatagramTransportModule` | `NewDatagramDialer`, `NewDatagramListener` |
| `OverlayDatagramTransportModule` | `WrapDatagramDialer`, `WrapDatagramListener` |

**Proxy kinds.** What sits below a protocol (its underlying carrier) and what it
returns above (its payload) are independent, so there are three shapes:

| Kind | Below (underlying) | Above (payload) | Example |
| --- | --- | --- | --- |
| `StreamProxyModule` | `Stream` | `Stream` | SOCKS5, HTTP and Trojan CONNECT (implemented) |
| `DatagramOverStreamProxyModule` | `Stream` | `Datagram` | Trojan and VLESS UDP (implemented) |
| `DatagramProxyModule` | `Datagram` | `Datagram` | Shadowsocks UDP (none implemented) |

[![The three proxy shapes: StreamProxyModule takes an underlying Stream and returns a Stream payload (SOCKS5, HTTP, Trojan); DatagramOverStreamProxyModule takes a Stream and returns a Datagram (Trojan UDP; VLESS); DatagramProxyModule takes a Datagram and returns a Datagram (Shadowsocks UDP, none yet).](assets/diagrams/proxy-shapes.excalidraw.svg)](assets/diagrams/proxy-shapes.excalidraw.svg)

A proxy protocol never opens a connection. It is handed an underlying stream or
datagram and returns a payload ("wrapper, not pump"); core runs the copy loop.
A server's `Accept*` may return `ErrFallbackRequested` for a request that is not
its protocol, before writing anything; core then replays the request to the
inbound's fallback.

**Sniffer.** `SnifferModule` holds the settings and builds a `Sniffer` with
`NewSniffer()`; the `Sniffer` has `Sniff(payloadPrefix []byte) (SniffResult,
error)`, returning `ErrSniffNeedMoreData` while the bytes so far are only a
prefix.

**API.** `APIModule` holds the settings, which include the address it listens on, and builds an
`APIServer` with `NewAPIServer()`. `Start(ctx, coreProvider)` binds and returns once listening;
`Close()` stops and waits. The server reaches the running core only through one
`APICoreProvider`: small questions about statistics, live conversations, the log, outbounds, a delay
test, a selector, a DNS lookup, the routing rules and reloading the configuration. `core/apicoreprovider` implements it, converting core's types to `spec`'s; the
API imports `spec` and nothing from core.

**Core-facing declarations.** `Metadata` (network, destination, source, inbound
tag, user, sniff result), and for SOCKS5's second socket
`UDPAssociateSocketFunc` plus the optional `UDPAssociateHandler` a proxy server
implements to receive it. `Router`, `TrafficHandler`, `Inbound` and `Outbound`
are not module contract: modules never use them, so they live in
`core/component`.

Every module kind embeds `Config` (`Validate() error`, `Warnings() []string`) and
has `Info() ModuleInfo`. The module value *is* its configuration: core decodes the
config file's `settings` straight into it, so `spec` never names a config field.

## Modules

A module is a package under `module/` that exports a constructor whose return
type is the kind it fills (a proxy protocol that fills several returns their
combination, and is registered once):

```go
func NewBaseStreamTransportModule() spec.BaseStreamTransportModule       // module/transport/base/tcp
func NewOverlayStreamTransportModule() spec.OverlayStreamTransportModule // module/transport/overlay/tls
func NewProxyModule() spec.StreamProxyModule                             // module/proxy/socks5
func NewProxyModule() spec.StreamAndDatagramOverStreamProxyModule        // module/proxy/trojan, vless
func NewSnifferModule() spec.SnifferModule                               // module/sniffer/tls
```

The module value is the settings; its `New...` methods build the running parts
once, per role (a TLS server loads its certificates there).

`core/registry` stores these constructors by name, and for a proxy protocol one entry per name that shows the one built value under each kind it fills. `registry.Default()`
in `core/registry/default.go` is the only place in the project that names a
module. There is no `init()` and no global registry, so a Go program using Apranik can build a
registry containing modules Apranik has never heard of.

[![Module registration: core/registry/default.go is the only file naming modules and registers tcp, udp, tls, ws, httpupgrade, fragment, socks5, http, trojan (twice: stream and datagram-over-stream) and the tls and http sniffers into typed tables for transports, proxy protocols and sniffers. Every registration is checked; config.Parse looks modules up by name and position.](assets/diagrams/module-registry.excalidraw.svg)](assets/diagrams/module-registry.excalidraw.svg)

## Stack composition

A transport stack is written base first, then overlays outward from the wire.
Only the base makes a dialer or listener. Each stream overlay wraps one
connection at a time,
so the builder composes a stack without knowing what any layer is:

- **Outbound.** The builder asks the base for a dialer and each overlay for its
  client, and joins them into one `StreamDialer`: dial through the base, then
  `WrapClientStream` in stack order. Each layer bounds its own part: `tcp` with
  its `dial_timeout`, each overlay with its own `handshake_timeout`.
- **Inbound.** The listener is the base alone. `core/inbound` accepts, counts the
  connection against `max_tcp_connections`, and starts a goroutine for it; there it
  runs each overlay's `WrapServerStream`, then the proxy server, each within its
  own `handshake_timeout`. A client that stalls a TLS handshake holds up
  only its own goroutine.

[![Stack composition. Inbound, reading up from the wire: the client connection, the tcp base listener that returns at once, overlays (tls, ws, httpupgrade) wrapping the connection in its own goroutine within handshake_timeout, the proxy server returning payload and Metadata, then core/inbound handing the stream to the handler, where the router chooses an outbound or blocks. Proxy outbound, writing down: the proxy client, overlays wrapping the dialed stream within dial_timeout, the tcp base transport, then the proxy server. Freedom has no stack. Block is not an outbound: it is a routing action.](assets/diagrams/stack-composition.excalidraw.svg)](assets/diagrams/stack-composition.excalidraw.svg)

[![Stream overlays run per connection: the accept loop only accepts, takes a max_tcp_connections slot and starts a goroutine; that goroutine runs the tls and ws server sides, the proxy server and the handler, bounded by handshake_timeout. A silent client holds only its own goroutine. A failed overlay handshake is closed; fallback recording starts above the overlays. On a proxy outbound the same layers run after the base dials.](assets/diagrams/overlay-per-connection.excalidraw.svg)](assets/diagrams/overlay-per-connection.excalidraw.svg)

Protocols that carry many streams over one connection (HTTP/2, gRPC) do not fit
this shape and will get their own kind (open question Q17). Datagram overlays
still wrap dialers and listeners; none exists yet.

## Shared helper packages

Code several modules need that is not part of the contract lives under
`shared/`, one concept per package. Modules may import these, `spec` and
`config/types`, and never another module. A package is added only once two
real modules need it:

| Package | Concept | Used by |
| --- | --- | --- |
| `shared/socksaddr` | the RFC 1928 address codec: ATYP, address, port | `socks5`, `trojan` |
| `shared/vmessaddr` | the V2Ray family's address codec: port, type (1 IPv4, 2 domain, 3 IPv6), address | `vless` (`muxcool` below) |
| `shared/muxcool` | Mux.Cool: sub-connections (TCP, UDP and XUDP) over one established stream, both ends, with limits and idle close | `vless` (XUDP) |
| `shared/delayedrequest` | a stream that sends a proxy client's request together with the first payload, or alone after a maximum delay | `trojan`, `vless` |
| `shared/websocket/handshake` | the HTTP/1.1 WebSocket upgrade handshake: size limit, context bound, web-server refusals, Host handling | `ws`, `httpupgrade` |

## Core packages

| Package | Responsibility |
| --- | --- |
| `core/component` | the roles core's own parts play for each other: `Inbound`, `Outbound`, `Router` (with its `RouterTrafficInformant` and `Decision`), `TrafficHandler`; modules never use them |
| `core/registry` | name → module constructor, per kind (a proxy protocol is one entry under all the kinds it fills), with clear errors for a name used in the wrong position |
| `core/blueprint` | a validated description of every inbound, outbound and routing rule; nothing is opened |
| `core/builder` | blueprint → `component.Inbound` / `component.Outbound` / `component.Router`; builds each module's components, joins an outbound's dialer and overlay clients; grants the UDP associate socket; loads datasets for the router |
| `core/inbound` | accept loop, per-connection overlay handshakes, handshake timeout, identifying which payload a request asks for, fallback, handing a UDP association to the handler |
| `core/inbound/mixed` | the special inbound protocol `mixed`: a `spec.StreamProxyServer` that picks the SOCKS5 or HTTP server from the first byte |
| `core/apicoreprovider` | the one `spec.APICoreProvider` every API module gets: statistics, live conversations and closing them, a log subscription, outbound state, a delay test, choosing a selector's member, a DNS lookup and flushing its cache, the routing rules as text, asking for a reload |
| `core/sniff` | peeks the start of a stream through `core/net/peekable`, runs sniffers; called by the handler when a rule's `sniff` action asks |
| `core/net/peekable` | `spec.PeekableStream`; a recording mode that replays a refused handshake to the fallback |
| `core/route/router` | `component.Router`: rules compiled once, evaluated in order, first decision wins; shared by every inbound |
| `core/route/router/match` | the lookup structures rules compile into: domains (exact, suffix by label, keyword, regex), IP ranges (sorted, merged, binary search), ports; `ip_is_private` |
| `core/route/dataset` | loads datasets, reading each file once; `dataset/geodata` decodes V2Ray's `geosite.dat` and `geoip.dat` with the standard library |
| `core/route/handler` | `component.TrafficHandler`: dials, relays; owns each UDP session and its routing cache |
| `core/route/informant` | the handler's `component.RouterTrafficInformant`s: `Stream` (sniffs when a rule asks, replays what it peeked) and `UDPSession`; both resolve the destination when a rule asks |
| `core/relay` | `relay.Stream` copies both ways and carries half-close; `relay.Datagrams` moves packets between a client and its UDP session |
| `core/outbound` | the proxy outbound (dialer stack + proxy client; UDP too when the protocol frames it in streams), freedom and the delay test |
| `core/outbound/group` | the outbound groups `urltest`, `fallback` and `selector` |
| `core/net/udp/session` | one UDP session: one datagram per outbound, replies merged, closed on silence |
| `core/net/udp/resumable` | keeps a UDP session after its stream is lost and gives it to the stream that comes back with the same Global ID |
| `core/net/udp/nat` | freedom's UDP: which local socket carries each destination, and which senders may reply (RFC 4787) |
| `core/logging` | eight RFC 5424 levels over `log/slog`, fanned out to every output, the live subscription hub and the sampler |
| `core/logging/sink` | the outputs: stderr, file, memory and syslog, each a `logging.Output` |

## UDP design

UDP has no connection, so one client flow can reach many destinations, each
possibly through a different outbound. Three pieces share the work:

- **The handler routes.** For each packet the relay asks the handler which outbound
  to use. The handler caches the answer per destination for the life of the UDP
  session (`udpSessionRoutingCache`), so the router is asked once per new
  destination. A block is cached like any decision; failed routing is not.
- **The session holds sockets.** One `session.Session` per client flow, created by
  the handler. For each outbound the flow reaches it opens one datagram, merges
  replies from all of them, and closes everything together. It routes nothing.
- **The NAT sits behind freedom.** `nat.Datagram` decides which local socket carries
  each destination and which senders may reply, using RFC 4787 mapping and
  filtering. Replies to a domain are reported back under the domain the client used.
- **A proxy outbound frames UDP in a stream.** When its protocol has a
  datagram-over-stream kind (Trojan, VLESS), `CreateDatagram` dials the server
  through the stack, overlays included, and returns a `Datagram` whose messages
  are framed inside a stream. Where the protocol writes the destination decides how
  many streams: Trojan names it in every datagram, so one stream carries a UDP
  session's datagrams to every destination; VLESS names it once, in the stream
  request, so the outbound opens one stream per destination
  (`datagramOverStreamPerDestination`) and the session still sees one datagram.
  The client says which with `DatagramDestinationIsWritten`.

Because each UDP session has its own NAT, two clients never receive each other's
replies. Timeouts are PROVISIONAL and configured beside what they bound: a silent
session ends after the inbound's `udp_session_idle_timeout` (5 minutes), a silent
NAT socket closes after freedom's `nat.idle_timeout` (5 minutes), and a cached
route is forgotten after the inbound's `udp_routing_cache_entry_idle_timeout` (1 minute).

[![One UDP session: client A's SOCKS5 association feeds relay.Datagrams, which asks the per-session routing cache for a decision, an outbound or a block, and sends through session.Session; the session holds one datagram per outbound, such as freedom's nat.Datagram with its local socket reaching several destinations. The router is shared. Client B has its own session and NAT, so replies never cross.](assets/diagrams/udp-session-structure.excalidraw.svg)](assets/diagrams/udp-session-structure.excalidraw.svg)

## Package dependency graph

Drawn from `go list` imports (the `cmd/apranik` main package is left out).
Edges to `spec`, `core/logging` and `core/component` are omitted: nearly every
package imports `spec`, nine import `core/logging` and eight import
`core/component`. Modules are grouped, since only `core/registry` (and
`core/builder`, for `tcp` and `udp`) imports them.

[![Package dependency graph generated from go list, in layers: cmd/apranik/cli imports config, core/builder, core/registry, core/blueprint and the handler; config imports registry, blueprint, config/types, nat, route/dataset and route/match; core/builder imports blueprint, registry, inbound, inbound/mixed, outbound, route/router and route/dataset; route/router imports blueprint, route/dataset and route/match; route/dataset imports blueprint, dataset/geodata and route/match; the handler imports sniff, peekable, relay and session; inbound imports peekable and relay; nat, session and relay import core/net/udp. Modules and shared packages sit below, imported only by core/registry, and core/builder for tcp and udp. Edges to spec, core/logging and core/component are left out.](assets/diagrams/package-dependencies.excalidraw.svg)](assets/diagrams/package-dependencies.excalidraw.svg)

Three edges worth knowing: `core/builder` imports `module/transport/base/udp`
directly, to open SOCKS5 associate sockets without a registry lookup, and
`module/transport/base/tcp`, because a fallback is always dialed over plain TCP;
`cmd/apranik/cli` imports the router and handler because it wires them
together.
