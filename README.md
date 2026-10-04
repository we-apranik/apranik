# Apranik

An open-source proxy core for censorship circumvention, written in Go.

Apranik is a **core, not a GUI**. Its main product is the `apranik` binary, which
client apps (Hiddify, v2rayNG, NekoBox, v2box) and server panels run as they run
any proxy core: they write a config file and start the binary. Its Go packages
can also be used directly as a library.

## Why it exists

The motivating environment is Iran, where censorship changes faster than proxy
cores release. What matters there is not how many protocols a core ships, but how
quickly a new circumvention technique can be added.

So Apranik is built around one property: **a researcher who discovers a new
technique can ship it as one module, without touching anything else.** Adding a
module means a new folder and one line in the registry. If a technique needs
edits to the contract, to another module, or to the builder, the contract is
missing a concept; the fix is to add that concept, once and generally, never to
let one module know another.

[![From a technique to a deployed module: a researcher finds a technique, writes one folder under module/ implementing one spec kind, tests it without core, adds one line to core/registry/default.go, names it by "type" in a config file that apranik check validates, and ships it in a release of the apranik binary that clients and panels pick up (planned). spec, the builder, core's inbound and outbound, every other module and the config parser stay untouched.](docs/assets/diagrams/research-to-deployment.excalidraw.svg)](docs/assets/diagrams/research-to-deployment.excalidraw.svg)

Existing proxy cores are mature and useful. The claim here is narrower: their
architecture is not optimised for rapid, Iran-specific adaptation.

## Principles

- **One technique, one module.** A new technique is a new folder under
  `module/` plus one line in the registry. Nothing else changes.
- **A small, fixed contract.** `spec` holds every interface and imports nothing
  from the project. Modules import `spec`, never core and never each other, so a
  module could live in someone else's repository.
- **Protocols and transports stay apart.** A proxy protocol never opens a
  connection; it is handed one. Transports compose as stacks, base first, each
  overlay wrapping the one below.
- **Explicit, not magic.** Modules are registered by name in one file; no
  `init()`, no globals. A Go program can register modules core has never seen.
- **Refuse bad config at startup.** Every problem is reported at once, with its
  location, before anything listens. Unknown keys are refused by default.
- **Safe defaults.** A listen address is required, sniffing is off until asked
  for, an inbound without credentials is warned about, and secrets never appear
  in error text.
- **Standard library first.** Each dependency must be justified; the one so far
  is uTLS, for browser ClientHellos.
- **A core, not an app.** Apranik ships a binary and a library; the user
  interface belongs to the clients and panels that run it.

## At a glance

[![System overview: a JSON config file is parsed and checked into a blueprint, which the builder turns into inbounds and outbounds. At run time a client connects to an inbound (a transport stack plus a proxy server: socks5, http, trojan or mixed, with optional sniffing), the handler asks the router for an outbound and the relay copies traffic to the destination.](docs/assets/diagrams/system-overview.excalidraw.svg)](docs/assets/diagrams/system-overview.excalidraw.svg)

## Status

Early and working. Two dependencies: uTLS, and `golang.org/x/net` for its DNS message codec.

| Area | Available |
| --- | --- |
| Transports | `tcp` (base stream), `udp` (base datagram), `tls` (Chrome's ClientHello by default, via uTLS), `ws` and `httpupgrade` (stream overlays) |
| Proxy protocols | `socks5` (CONNECT, UDP ASSOCIATE, RFC 1929 auth), `http` (CONNECT, absolute-form, Basic auth), `trojan` (CONNECT and UDP, named users, fallback; tested against Xray over `tls`, `ws` and `httpupgrade`) |
| Inbounds | any proxy protocol over any stream transport, and `mixed` (SOCKS5 and HTTP on one port) |
| Outbounds | proxy outbounds over any stream transport stack (UDP too, for Trojan), `freedom` (straight to the destination) and `block` (refuses everything) |
| Sniffers | `tls` (SNI from a ClientHello), `http` (Host header), per inbound, off by default |
| UDP | per-destination routing, one NAT per UDP session, RFC 4787 mapping and filtering |
| DNS | servers `udp`, `tcp`, `dot`, `doh` and the operating system's, each through any outbound; DNS rules, a TTL cache, IPv4/IPv6 preference, and a routing `resolve` action ([docs](docs/configuration/dns/index.md)) |
| Configuration | JSON config file, validated in full before anything starts |

Not yet: multiplexing transports (HTTP/2, gRPC), VLESS, REALITY, ECH, routing rules
(every request uses the first outbound), sniffing UDP, and QUIC.

Complete server and client configs for Trojan over TLS, WebSocket and
HTTPUpgrade are in [docs/configuration/examples.md](docs/configuration/examples.md).

## Quick start

Requires Go 1.27 or newer.

```sh
go build -o apranik ./cmd/apranik
./apranik check -config cmd/apranik/config.json
./apranik run   -config cmd/apranik/config.json
```

The example config opens a SOCKS5 inbound on `127.0.0.1:1080` and an HTTP
inbound on `127.0.0.1:1081`, both sending traffic out through `freedom`:

```sh
curl -x socks5h://127.0.0.1:1080 https://example.com
curl -x http://127.0.0.1:1081    https://example.com
```

## Command line

```
apranik run      [-config source] [-fetch-timeout d]   load a config file and serve it
apranik check    [-config source] [-fetch-timeout d]   check a config file without running it
apranik modules                     list the modules compiled into this build
apranik generate uuid               generate a value a config file needs
apranik version                     version and build information
apranik help     [command]          usage
```

`-config` takes a file path or an `http(s)` URL, and defaults to `config.json`.
`-fetch-timeout` bounds the download of a URL config file (default `30s`).
`check` runs every validation `run` does, without binding a port, so a config
can be checked on a machine that could never run it.

## Configuration

A config file has a log section, inbounds (how traffic arrives) and outbounds (how
it leaves). Inbounds and outbounds share one shape: a `transport` stack and a
`protocol`, each written as `{ "type": ..., "settings": {...} }`. Two outbound
protocols are special: `freedom` and `block` take no server; `block` takes no
transport, and `freedom`'s holds only `tcp` and transports that need no server
side, such as `fragment`.
One inbound protocol is special: `mixed` serves SOCKS5 and HTTP on one port.

```json
{
  "version": 1,
  "log": { "level": "info", "outputs": [{ "type": "stderr" }] },

  "inbounds": [
    {
      "tag": "socks-in",
      "listen": "127.0.0.1",
      "port": 1080,
      "transport": [{ "type": "tcp" }],
      "protocol": { "type": "socks5", "settings": { "udp_relay": true } }
    }
  ],

  "outbounds": [
    { "tag": "direct", "protocol": { "type": "freedom" } },
    { "tag": "drop",   "protocol": { "type": "block" } },
    {
      "tag": "upstream",
      "server": "203.0.113.5",
      "server_port": 1080,
      "transport": [{ "type": "tcp" }],
      "protocol": { "type": "socks5" }
    }
  ]
}
```

Every problem is reported at once, each with its location, rather than stopping
at the first. Unknown keys are refused by default, because a misspelled key
silently ignored is a setting the user believes is in effect and is not; set
`"strict": false` to relax that.

### Top level

| Key | Meaning |
| --- | --- |
| `version` | config file format version; currently `1` |
| `min_core` | oldest Apranik version the config file needs |
| `strict` | refuse unknown keys; default `true` |
| `log` | see below |
| `inbounds`, `outbounds` | arrays, see below |
| `datasets` | named lists of domains or IP ranges that routing and DNS rules look up |
| `dns` | the resolver: DNS servers, DNS rules, cache; absent means the operating system resolves |
| `routing` | rules choosing an outbound per request |

### Inbound

| Key | Meaning |
| --- | --- |
| `tag` | name used in logs |
| `listen`, `port` | IP address and port to listen on; an address is required, so nothing is exposed on every interface by omission |
| `network` | `tcp` (default) or `udp` |
| `tcp_idle_timeout` | a TCP connection silent in both directions for this long is closed; default `5m`, negative disables |
| `max_connections` | connections held at once; one more is closed as soon as it is accepted; default `0`, no limit |
| `udp_session_idle_timeout` | a UDP session silent for this long is ended; default `5m` |
| `transport` | the transport stack, base first, e.g. `[{ "type": "tcp" }, { "type": "tls", ... }]` |
| `protocol` | the proxy protocol, `{ "type": ..., "settings": {...} }`, or the special protocol `mixed` |
| `sniffing` | absent means off; see below |
| `fallback` | `{ "server", "server_port" }`: where a request the protocol refuses is replayed (Trojan) |

### Sniffing (`inbounds[].sniffing`)

```json
"sniffing": { "sniffers": [{ "type": "tls" }, { "type": "http" }], "timeout": "300ms" }
```

After the handshake, the inbound peeks at the client's first bytes and asks
each sniffer whether it recognises them. A match fills the request's sniffed
protocol and domain before routing; the destination itself is never
rewritten, and the peeked bytes are replayed so the outbound sees everything.
`timeout` (default `300ms`) bounds the wait for a client that does not speak
first, such as SSH, which is why sniffing is off unless configured. TCP
inbounds only, for now.

### Outbound

| Key | Meaning |
| --- | --- |
| `tag` | name used in logs |
| `protocol` | a proxy protocol such as `socks5`, `http` or `trojan`, or one of the special protocols `freedom` and `block` |
| `server`, `server_port` | the proxy server to reach; proxy protocols only |
| `transport` | the transport stack used to reach the server; proxy protocols only |
| `dns_server` | the DNS server that resolves `server` when it is a domain; proxy protocols only |

### Special inbound protocol

**`mixed`** serves SOCKS5 and HTTP on one port, choosing per connection from
the first byte. Its settings are the two modules' own settings, and either may
be left out for that module's defaults; `first_byte_timeout` (default `10s`)
bounds the wait for the byte that chooses:

```json
{ "tag": "mixed-in", "listen": "127.0.0.1", "port": 1082,
  "transport": [{ "type": "tcp" }],
  "protocol": { "type": "mixed", "settings": {
    "socks5": { "username": "u", "password": "p", "udp_relay": true },
    "http":   { "username": "u", "password": "p" }
} } }
```

Each half keeps its own credentials; a half without a username is warned about
at its own path, such as `protocol.settings.http`. TCP inbounds only.

### Special outbound protocols

**`freedom`** sends traffic straight to each destination over TCP and UDP. It
takes no server. Its optional `transport` is how its TCP reaches destinations:
`tcp` first, then only transports that need no server side, such as `fragment`
(a destination runs none of Apranik's layers, so `tls` or `ws` there is
refused). Left out, it is plain `tcp`. Its settings configure its UDP and the
NAT behaviour of that UDP:

```json
{ "tag": "direct",
  "transport": [{ "type": "tcp", "settings": { "keep_alive": "15s" } }, { "type": "fragment" }],
  "protocol": { "type": "freedom", "settings": {
    "nat": { "filtering": "address-and-port-dependent", "idle_timeout": "5m" }
} } }
```

| `settings` key | Meaning |
| --- | --- |
| `udp` | the `udp` module's settings (see Module settings) |
| `nat.mapping` | `endpoint-independent` (default), `address-dependent`, `address-and-port-dependent` |
| `nat.filtering` | same three; default `address-and-port-dependent` |
| `nat.idle_timeout` | how long one NAT socket may stay silent before it is closed; default `5m` |

**`block`** refuses every connection and drops every UDP packet sent to it. It
takes no settings, server, transport or timeouts.

The NAT names come from RFC 4787. The default filtering is the strictest, and
choosing `endpoint-independent` filtering produces a warning, because it lets
anyone who can reach the socket inject packets into a session.

### Module settings

| Module | Settings |
| --- | --- |
| `tcp` | `keep_alive` (idle before the first probe, default `15s`, negative disables), `keep_alive_interval` (between probes, default `15s`), `keep_alive_count` (unanswered probes before the connection is dropped, default `9`), `no_delay` (default `true`), `dial_timeout` (default `10s`), `next_address_delay` (default `250ms`: how long one address is tried before the next starts beside it) |
| `udp` | none; a domain is resolved through `dns` |
| `tls` | client: `server_name`, `insecure`, `certificate_authorities`, `fingerprint` (default `chrome`; `none` for Go's own); server: `certificates` (file or inline PEM); both: `alpn` (`["http/1.1"]` under `ws` and `httpupgrade`), `min_version`, `max_version` (a client only with `fingerprint: none`), `handshake_timeout` (default `10s`) |
| `ws` | `path` (default `/`), `host` (client: the Host header; server: a required Host), `headers`, `early_data` (`max_bytes`, required; `location` `header` (default) or `path`; `header_name`, default `Sec-WebSocket-Protocol`), `handshake_timeout` (default `10s`) |
| `httpupgrade` | `path`, `host`, `headers`, `pipeline` (client, default `false`), `accept_websocket_key` (server, default `true`), `handshake_timeout` (default `10s`) |
| `fragment` | outbound only: `tls_records`, `tcp_packets` (both default `true`), `piece_sizes` (default `["3-5", "10-20"]`), `piece_delays` (default `["10-20ms"]`, only with `tcp_packets`), `max_pieces` (default `"4-8"`, `0` no limit), `split_sni` (default `true`) |
| `socks5` | `username`, `password`, `udp_relay` (default `true`), `handshake_timeout` (default `10s`) |
| `http` | `username`, `password`, `handshake_timeout` (default `10s`) |
| `trojan` | server: `users` (`[{ name, password }]`), `handshake_timeout` (default `10s`); client: `password` |

An inbound with no username is accepted with a warning: anyone who can reach it
may use it.

### Log

`level` is one of the eight RFC 5424 severities, from `debug` to `emergency`.
`outputs` is a list, so logs can go to several places at once, each
`{ "type": ... }` one of `stderr`, `file` (with `path`), `memory` (with
`capacity`), or `syslog` (with `network`, `address`, `tag`). An empty list is
silent.

`stderr` and `file` take a `format`: `logfmt`, `json`, or `human` (aligned
columns for a person at a terminal). `stderr` defaults to `logfmt`, because
the programs that run Apranik usually capture it; `file` defaults to `json`.
With `human`, `stderr` also takes `"color": true`; setting the `NO_COLOR`
environment variable turns color off regardless.

```json
"outputs": [{ "type": "stderr", "format": "human", "color": true }]
```

## How it works

### Layers

[![Layers and import direction: cmd/apranik imports config, config imports core, core's registry names the modules, modules may import shared helper packages, and everything implements or uses spec. Every layer imports spec directly; spec imports nothing from the project. Modules also use config/types for shared JSON values.](docs/assets/diagrams/architecture-overview.excalidraw.svg)](docs/assets/diagrams/architecture-overview.excalidraw.svg)

| Layer | Holds | May import |
| --- | --- | --- |
| `spec` | the module contract: interfaces, address types, sentinel errors; no state | nothing from the project |
| `shared` | helpers two modules need, one concept each (`socksaddr`, `websocket/handshake`) | `spec` and the standard library |
| `module` | transports, proxy protocols and sniffers, one folder each | `spec`, `shared`, and `config/types` for shared JSON values |
| `core` | building, routing, relaying, UDP sessions and NAT | `spec`, `shared` and modules |
| `config` | JSON config file to a blueprint | `spec`, `core` |

Modules never import core. A module is a folder that could live in someone
else's repository.

### The contract

- **Two carriers.** `Stream` is `net.Conn` plus `CloseWrite`, so half-close is part
  of the contract rather than a type assertion. `Datagram` keeps message
  boundaries.
- **Two address types.** `Endpoint` is an IP and a port, already resolved.
  `Destination` is exactly one of a domain or an IP, plus a port. A domain stays
  a domain all the way down: **only the base transport resolves**, through the
  resolver core hands it, so a poisoned local DNS answer never reaches a proxy
  protocol's decisions.
- **Base and overlay transports.** A base transport creates a connection (TCP,
  UDP); an overlay wraps one (TLS, WebSocket), one connection at a time, inside
  core's timeouts. A stack is a fold core runs without knowing what any layer
  is.
- **Three proxy shapes**, because what sits below a protocol and what it returns
  are independent: `Stream → Stream`, `Stream → Datagram` (UDP over a stream, as
  Trojan and VLESS do), and `Datagram → Datagram` (native UDP). A proxy protocol
  never opens a connection; it is handed an underlying stream.
- **Wrapper, not pump.** A proxy protocol returns a stream or datagram and core
  copies the bytes, so protocols and transports stay the same shape.

### A TCP connection

[![Sequence of one TCP connection: the client connects to core/inbound, the socks5 server negotiates and returns the payload stream and Metadata, core/sniff optionally peeks and replays, the handler asks the router, freedom dials the destination through the tcp module, and core/relay copies bytes both ways with half-close.](docs/assets/diagrams/tcp-lifecycle.excalidraw.svg)](docs/assets/diagrams/tcp-lifecycle.excalidraw.svg)

1. An inbound accepts a connection on its base transport and, in that
   connection's own goroutine, runs any overlays (a TLS handshake, a WebSocket
   upgrade).
2. The proxy protocol reads the handshake and returns the payload and its
   `Metadata`: where the client wants to go.
3. The traffic handler asks the **router** for an outbound.
4. The outbound reaches the destination, directly (freedom) or through its
   proxy server.
5. The relay copies both ways and carries half-close across.

### UDP

UDP has no connection, so a single client flow can reach many destinations,
each possibly through a different outbound. Three pieces handle that:

[![One UDP session: client A's SOCKS5 association feeds relay.Datagrams, which asks the per-session routing cache for an outbound and sends through session.Session; the session holds one datagram per outbound, such as freedom's nat.Datagram with its local socket reaching several destinations. The router is shared. Client B has its own session and NAT, so replies never cross.](docs/assets/diagrams/udp-session-structure.excalidraw.svg)](docs/assets/diagrams/udp-session-structure.excalidraw.svg)

- **The handler routes.** For each packet the relay asks the handler which
  outbound to use; the handler asks the router once per *new destination* and
  caches the answer for the life of the UDP session. The relay only moves packets.
- **The session** is one client's flow, such as one SOCKS5 association. For each
  outbound the flow reaches, it opens one datagram and remembers it. It merges
  replies from all of them and closes everything together. It routes nothing.
- **The NAT** (`core/net/udp/nat`) sits behind freedom. It decides which local
  socket carries each destination and which senders may reply, in RFC 4787
  terms. Replies to a domain are matched against the address the domain actually
  resolved to, and reported back under the domain the client used.

Each client flow has its own session and so its own NAT, so two clients never
see each other's replies. Silence ends things: each NAT socket closes after the
freedom outbound's `nat.idle_timeout`, and the whole session after the inbound's
`udp_session_idle_timeout`.

## Adding a module

A module is a package under `module/` that implements one of the `spec` kinds and
exports a constructor per kind:

```go
func NewBaseStreamTransportModule() spec.BaseStreamTransportModule
```

Register it with one line in `core/registry`, and it is available to config files by
name. It gets tests that run without core, since the contract is all it depends
on.

## Documentation

The documentation site lives in `docs/` and is built with MkDocs and the Material
theme (Python 3 required):

```sh
python3 -m venv .venv
.venv/bin/pip install -r requirements-docs.txt

.venv/bin/mkdocs serve              # live preview at http://127.0.0.1:8000
.venv/bin/mkdocs build --strict     # build the static site into site/
```

`mkdocs build --strict` fails on any broken link or warning; it is what CI runs
before publishing to GitHub Pages (started by hand while the repository is
private). See `docs/contributing.md` for how to edit the
pages and the diagrams.

## Licence

[Mozilla Public License 2.0](LICENSE).
