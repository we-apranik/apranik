# Glossary

| Term | Meaning |
| --- | --- |
| **ALPN** | Application-Layer Protocol Negotiation: the list of protocols (`h2`, `http/1.1`) a TLS client offers and the one the server picks. `tls`'s `alpn` sets it; `apranik check` refuses a list the transport above cannot speak. |
| **Base transport** | A transport that creates connections from nothing: `tcp` and `udp`. Implements `spec.BaseStreamTransportModule` or `spec.BaseDatagramTransportModule`. Also called an underlay. |
| **Block** | The routing action that refuses a request: a TCP connection is closed, UDP datagrams are dropped. |
| **Blueprint** | A `blueprint.Blueprint`: every inbound and outbound with its modules looked up, decoded and validated, but nothing opened. |
| **Builder** | `core/builder`: turns a blueprint into running inbounds and outbounds. It builds each module's components and joins a proxy outbound's base dialer and overlay clients into one dialer. |
| **Carrier** | A `spec.Stream` or `spec.Datagram` handed from one layer to the next. When the kind is known, Apranik says "stream" or "datagram", and "underlying stream" for the layer below. |
| **CDN fronting** | Reaching a server through a CDN, so a censor sees the CDN's address and TLS name. `ws` and `httpupgrade` exist for it. See [Examples](configuration/examples.md#trojan-over-websocket-behind-a-cdn). |
| **Component** | `core/component`: the roles core's own parts play for each other (`Inbound`, `Outbound`, `Router`, `TrafficHandler`). Not module contract, so not in `spec`. |
| **Core** | Everything under `core/`: the code that composes modules. Modules never import it. |
| **Dataset** | A named list of domains or IP ranges, read from a file such as `geosite.dat`, that routing and DNS rules look up. Values only; what to do with them is in the rules. Declared at the top level of the config. |
| **Datagram** | The message-oriented carrier. Apranik says "datagram", never "packet", in code. |
| **DNS server** | One configured upstream the resolver sends queries to (`dns.servers[]`): a module of type `udp`, `tcp`, `dot` or `doh` carrying wire-format messages over its outbound, or the special type `system`, the operating system. Implements `spec.StreamDNSServerModule` or `spec.DatagramDNSServerModule`. |
| **Destination** | `spec.Destination`: exactly one of a domain or an IP, plus a port. Resolved only by a base transport. |
| **Diagnostics** | The list of errors and warnings `config` reports, each with its location in the config file. |
| **Early data** | Payload sent inside a handshake to save a round trip: `ws` puts the start of the first write in a header (`early_data`, Xray's `ed`). |
| **Endpoint** | `spec.Endpoint`: an IP and a port, already resolved. |
| **Fallback** | Where an inbound sends a request its protocol refuses with `spec.ErrFallbackRequested`: every byte, from the first, is replayed to that address over plain TCP, so a probe sees an ordinary service. |
| **Fingerprint** | What a TLS ClientHello reveals about the program that sent it: cipher suites, extensions, their order. `tls`'s `fingerprint` picks a browser's to imitate through uTLS. |
| **Fragment** | The overlay transport that cuts the TLS ClientHello into records, packets or both, so DPI that does not reassemble never sees the server name whole. Outbound only. |
| **Freedom** | The special outbound protocol that sends straight to each destination, over TCP (its transport: `tcp`, then only transports that need no server side) and UDP. |
| **Handler** | `core/route/handler`: the `component.TrafficHandler` every inbound calls. It asks the router, dials, relays, and owns UDP sessions. |
| **Inbound** | How traffic enters: a listener stack plus a proxy server (`component.Inbound`). |
| **Lookup** | What the resolver does: a name in, addresses out. A DNS server module does not look up; its `Query` carries one wire-format message out and one back. |
| **Metadata** | `spec.Metadata`: network, destination, source, inbound tag, user and sniff result. Crosses the proxy layer, and only there. |
| **Mixed** | The special inbound protocol that serves SOCKS5 and HTTP on one port, choosing per connection from the first byte. Built by core in `core/inbound/mixed` from the `socks5` and `http` modules. |
| **Module** | A folder under `module/` that implements one or more `spec` kinds and exports a `New*Module` constructor per kind. The value the constructor returns is the module's settings; its `New...` methods build the running parts. |
| **Module kind** | One of the `spec` interfaces a module fills, each named `...Module`: `BaseStreamTransportModule`, `OverlayStreamTransportModule`, `StreamProxyModule`, `SnifferModule` and so on. |
| **NAT** | `core/net/udp/nat`: how freedom maps a UDP session onto real sockets and filters replies, in RFC 4787 terms. |
| **Outbound** | How traffic leaves: a proxy client plus a dialer stack, or freedom (`component.Outbound`). |
| **Overlay transport** | A transport that wraps a layer below it, one connection at a time (`WrapClientStream`, `WrapServerStream`), such as TLS or WebSocket. `tls`, `ws` and `httpupgrade` are built. |
| **Payload** | What a layer returns upward: the proxied traffic with the protocol's framing removed. |
| **Peekable stream** | `spec.PeekableStream`: a stream whose next bytes can be looked at without consuming them. `Peek` may read ahead; `PeekExactly` never reads past what was asked. Core implements it in `core/net/peekable`. |
| **Pipelining** | `httpupgrade`'s `pipeline`: the client sends its payload right after the upgrade request, without waiting for the `101`. |
| **Protocol** | The `protocol` of an inbound or outbound in the config file: a proxy protocol, `freedom` on an outbound, or `mixed` on an inbound. |
| **Proxy protocol** | A module that speaks proxy semantics over a supplied carrier, such as SOCKS5, HTTP CONNECT or Trojan. It never opens a connection itself. Its server `Accept*`s requests; its client `Request*`s a payload. |
| **Resolver** | `core/dns`: answers a lookup (a name in, addresses out) by choosing a DNS server with the DNS rules, asking for the IP versions configured, and caching the answer for its TTL. Every base transport resolves through it, as `spec.Resolver`. |
| **Registry** | `core/registry`: maps the names used in config files to module constructors, per kind. `registry.Default()` is the only place that names modules. |
| **Relay** | `core/relay`: the copy loops. `relay.Stream` for TCP, `relay.Datagrams` for a UDP session. |
| **Request** | What a proxy client sends to ask the server for a payload. `Request*` methods connect nothing and may send nothing until the first write. |
| **Router** | `core/route/router`: runs the routing rules and decides where a request goes: an outbound, or block. Shared by every inbound, it holds no per-request state and asks the traffic informant for what it lacks. |
| **Router traffic informant** | `component.RouterTrafficInformant`: what the router asks about the traffic it is deciding on: its `Metadata`, and a sniff done only when a rule asks. The handler makes one per connection, so the router never touches the stream. |
| **Shared package** | A package under `shared/` that modules may import: one concept, no state, `spec` and the standard library only. |
| **SNI** | Server Name Indication: the host name a TLS client sends in the clear. The `tls` sniffer reads it; the `tls` transport sends it. |
| **Sniffer** | A module that recognises a protocol in the start of a payload and reports its domain, such as the TLS SNI or the HTTP `Host`. `SnifferModule` holds its settings and builds the `Sniffer`. |
| **Stack** | A list of transports, base first and overlays outward from the wire, composed as a fold. |
| **Transport info** | `spec.TransportInfo`: facts about a configured transport that only the config check reads, today the application protocols it offers or can speak. |
| **UDP session** | One client's UDP flow, such as one SOCKS5 association: `core/net/udp/session`. |
| **Underlying stream** | The stream a layer is handed by the layer below it, as in `WrapServerStream(ctx, underlyingStream)` or `AcceptStream(ctx, underlyingStream)`. |
| **uTLS** | The library (`github.com/refraction-networking/utls`) that sends a copy of a real browser's ClientHello; Apranik's one dependency. |
