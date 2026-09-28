# Glossary

| Term | Meaning |
| --- | --- |
| **Base transport** | A transport that creates connections from nothing: `tcp` and `udp`. Implements `spec.BaseStreamTransport` or `spec.BaseDatagramTransport`. Also called an underlay. |
| **Block** | The special outbound protocol that refuses every connection and drops every UDP packet. |
| **Blueprint** | A `blueprint.Blueprint`: every inbound and outbound with its modules looked up, decoded and validated, but nothing opened. |
| **Builder** | `core/builder`: turns a blueprint into running inbounds and outbounds by folding transport stacks. |
| **Carrier** | A `spec.Stream` or `spec.Datagram` handed from one layer to the next. |
| **Core** | Everything under `core/`: the code that composes modules. Modules never import it. |
| **Datagram** | The message-oriented carrier. Apranik says "datagram", never "packet", in code. |
| **Destination** | `spec.Destination`: exactly one of a domain or an IP, plus a port. Resolved only by a base transport. |
| **Diagnostics** | The list of errors and warnings `config` reports, each with its location in the config file. |
| **Endpoint** | `spec.Endpoint`: an IP and a port, already resolved. |
| **Fallback** | Where an inbound sends a request its protocol refuses with `spec.ErrFallbackRequested`: every byte, from the first, is replayed to that address over plain TCP, so a probe sees an ordinary service. |
| **Freedom** | The special outbound protocol that sends straight to each destination, over plain TCP and UDP. |
| **Handler** | `core/route/handler`: the `component.TrafficHandler` every inbound calls. It asks the router, dials, relays, and owns UDP sessions. |
| **Inbound** | How traffic enters: a listener stack plus a proxy server (`component.Inbound`). |
| **Metadata** | `spec.Metadata`: network, destination, source, inbound tag, user and sniff result. Crosses the proxy layer, and only there. |
| **Mixed** | The special inbound protocol that serves SOCKS5 and HTTP on one port, choosing per connection from the first byte. Built by core in `core/inbound/mixed` from the `socks5` and `http` modules. |
| **Module** | A folder under `module/` that implements one or more `spec` kinds and exports a `New*Module` constructor per kind. |
| **NAT** | `core/net/udp/nat`: how freedom maps a UDP session onto real sockets and filters replies, in RFC 4787 terms. |
| **Outbound** | How traffic leaves: a proxy client plus a dialer stack, or freedom (`component.Outbound`). |
| **Overlay transport** | A transport that wraps a layer below it, such as TLS or WebSocket. None is built yet. |
| **Peekable stream** | `spec.PeekableStream`: a stream whose next bytes can be looked at without consuming them. `Peek` may read ahead; `PeekExactly` never reads past what was asked. Core implements it in `core/net/peekable`. |
| **Payload** | What a layer returns upward: the proxied traffic with the protocol's framing removed. |
| **Protocol** | The `protocol` of an inbound or outbound in the config file: a proxy protocol, `freedom` / `block` on an outbound, or `mixed` on an inbound. |
| **Proxy protocol** | A module that speaks proxy semantics over a supplied carrier, such as SOCKS5, HTTP CONNECT or Trojan. It never opens a connection itself. Its server `Accept*`s requests; its client `Request*`s a payload. |
| **Registry** | `core/registry`: maps the names used in config files to module constructors, per kind. `registry.Default()` is the only place that names modules. |
| **Request** | What a proxy client sends to ask the server for a payload. `Request*` methods connect nothing and may send nothing until the first write. |
| **Relay** | `core/relay`: the copy loops. `relay.Stream` for TCP, `relay.Datagrams` for a UDP session. |
| **Router** | `core/route/router`: picks the outbound for a `Metadata`. Stateless; no rules exist yet. |
| **Sniffer** | A module that recognises a protocol in the start of a payload and reports its domain, such as the TLS SNI or the HTTP `Host`. |
| **Shared package** | A package under `shared/` that modules may import: one concept, no state, `spec` and the standard library only. |
| **Stack** | A list of transports, base first and overlays outward from the wire, composed as a fold. |
| **UDP session** | One client's UDP flow, such as one SOCKS5 association: `core/net/udp/session`. |
