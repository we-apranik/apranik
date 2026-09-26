# Apranik

An open-source proxy core for censorship circumvention, written in Go.

Apranik is a **core, not a GUI**. Its main product is the `apranik` binary, which
client apps (Hiddify, v2rayNG, NekoBox, v2box) and server panels run the way they
run Xray-core or sing-box: they write a config file and start the core. Its Go
packages can also be used directly as a library.

## Why it exists

The motivating environment is Iran, where censorship changes faster than proxy
cores release. What matters there is not how many protocols a core ships, but how
quickly a new circumvention technique can be added.

So Apranik is built around one property: **a researcher who discovers a new
technique can ship it as one module, without touching anything else.** Adding a
module means a new folder and one line in the registry. If a change needs edits
to the contract, to another module, or to the builder, the design has failed.

Xray, sing-box and mihomo are mature and useful. The claim here is narrower:
their architecture is not optimised for rapid, Iran-specific adaptation.

## Status

Early and working. Standard library only; no third-party dependencies.

| Area | Available |
| --- | --- |
| Transports | `tcp` (base stream), `udp` (base datagram) |
| Proxy protocols | `socks5` (CONNECT, UDP ASSOCIATE, RFC 1929 auth), `http` (CONNECT, absolute-form, Basic auth), `trojan` (CONNECT and UDP, named users, fallback; plaintext until TLS exists) |
| Inbounds | any proxy protocol over any stream transport, and `mixed` (SOCKS5 and HTTP on one port) |
| Outbounds | proxy outbounds over any stream transport, `freedom` (straight to the destination) and `block` (refuses everything) |
| Sniffers | `tls` (SNI from a ClientHello), `http` (Host header), per inbound, off by default |
| UDP | per-destination routing, one NAT per UDP session, RFC 4787 mapping and filtering |
| Configuration | JSON config file, validated in full before anything starts |

Not yet: overlay transports (TLS is next, and is the first real test of
composition), routing rules (every request uses the first outbound), sniffing
UDP, and QUIC.

## Quick start

Requires Go 1.26 or newer.

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
protocols are special: `freedom` and `block` take no server and no transport.
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

### Inbound

| Key | Meaning |
| --- | --- |
| `tag` | name used in logs |
| `listen`, `port` | IP address and port to listen on; an address is required, so nothing is exposed on every interface by omission |
| `network` | `tcp` (default) or `udp` |
| `handshake_timeout` | how long a client may take to finish the proxy handshake; default `10s`, negative disables |
| `tcp_idle_timeout` | a TCP connection silent in both directions for this long is closed; default `5m`, negative disables |
| `udp_session_idle_timeout` | a UDP session silent for this long is ended; default `5m` |
| `transport` | the transport stack, base first, e.g. `[{ "type": "tcp" }]` |
| `protocol` | the proxy protocol, `{ "type": ..., "settings": {...} }`, or the special protocol `mixed` |
| `sniffing` | absent means off; see below |

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
| `protocol` | a proxy protocol such as `socks5` or `http`, or one of the special protocols `freedom` and `block` |
| `server`, `server_port` | the proxy server to reach; proxy protocols only |
| `transport` | the transport stack used to reach the server; proxy protocols only |
| `dial_timeout` | how long to wait for a connection to the server (or, for freedom, the destination); default `10s`, negative disables; not with `block` |
| `handshake_timeout` | how long the proxy handshake with the server may take; proxy protocols only; default `10s`, negative disables |

### Special inbound protocol

**`mixed`** serves SOCKS5 and HTTP on one port, choosing per connection from
the first byte. Its settings are the two modules' own settings, and either may
be left out for that module's defaults:

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

**`freedom`** sends traffic straight to each destination over plain TCP and UDP.
It takes no server and no transport. Its optional settings configure those two
transports and the NAT behaviour of its UDP:

```json
{ "tag": "direct", "protocol": { "type": "freedom", "settings": {
    "tcp": { "keep_alive": "15s" },
    "udp": { "resolve_timeout": "5s" },
    "nat": { "filtering": "address-and-port-dependent", "idle_timeout": "5m" }
} } }
```

| `settings` key | Meaning |
| --- | --- |
| `tcp` | the `tcp` module's settings (see Module settings) |
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
| `tcp` | `keep_alive` (duration, default `15s`), `no_delay` (default `true`) |
| `udp` | `resolve_timeout` (default `5s`): longest a domain lookup may take; `resolve_cache_ttl` (default `30s`): how long a resolved domain is reused |
| `socks5` | `username`, `password`, `udp_relay` (default `true`) |
| `http` | `username`, `password` |

An inbound with no username is accepted with a warning: anyone who can reach it
may use it.

### Log

`level` is one of the eight RFC 5424 severities, from `debug` to `emergency`.
`outputs` is a list, so logs can go to several places at once, each
`{ "type": ... }` one of `stderr`, `file` (with `path`), `memory` (with
`capacity`), or `syslog` (with `network`, `address`, `tag`). An empty list is
silent.

## How it works

### Layers

[![Layers and import direction: cmd/apranik imports config, config imports core, core's registry names the modules, and modules implement spec. Every layer imports spec directly; modules may also import config/types. spec imports nothing from the project.](docs/assets/diagrams/architecture-overview.excalidraw.svg)](docs/assets/diagrams/architecture-overview.excalidraw.svg)

| Layer | Holds | May import |
| --- | --- | --- |
| `spec` | every interface and the address types; no state, no logic | nothing from the project |
| `module` | transports and proxy protocols, one folder each | `spec`, and `config/types` for shared JSON values |
| `core` | building, routing, relaying, UDP sessions and NAT | `spec` and modules |
| `config` | JSON config file to a blueprint | `spec`, `core` |

Modules never import core. A module is a folder that could live in someone
else's repository.

### The contract

- **Two carriers.** `Stream` is `net.Conn` plus `CloseWrite`, so half-close is part
  of the contract rather than a type assertion. `Datagram` keeps message
  boundaries.
- **Two address types.** `Endpoint` is an IP and a port, already resolved.
  `Destination` is exactly one of a domain or an IP, plus a port. A domain stays
  a domain all the way down: **only the base transport resolves**, so a poisoned
  local DNS answer never reaches a proxy protocol's decisions.
- **Base and overlay transports.** A base transport creates a connection (TCP,
  UDP); an overlay wraps one (TLS, WebSocket). Overlays wrap dialers and
  listeners, so a stack is a fold the builder runs without knowing what any
  layer is.
- **Three proxy shapes**, because carrier and payload are independent:
  `Stream → Stream`, `Stream → Datagram` (UDP over a stream, as VLESS does), and
  `Datagram → Datagram` (native UDP). A proxy protocol never opens a connection;
  it is handed a carrier.
- **Wrapper, not pump.** A proxy protocol returns a carrier and core copies the
  bytes, so protocols and transports stay the same shape.

### A TCP connection

[![Sequence of one TCP connection: the client connects to core/inbound, the socks5 server negotiates and returns the payload stream and Metadata, core/sniff optionally peeks and replays, the handler asks the router, freedom dials the destination through the tcp module, and core/relay copies bytes both ways with half-close.](docs/assets/diagrams/tcp-lifecycle.excalidraw.svg)](docs/assets/diagrams/tcp-lifecycle.excalidraw.svg)

1. An inbound accepts a connection through its transport stack.
2. The proxy protocol reads the handshake and returns the payload and its
   `Metadata`: where the client wants to go.
3. The traffic handler asks the **router** for an outbound.
4. The outbound reaches the destination, directly (freedom) or through its
   proxy server.
5. The relay copies both ways and carries half-close across.

### UDP

UDP has no connection, so a single client flow can reach many destinations,
each possibly through a different outbound. Three pieces handle that:

[![One UDP session: the client's SOCKS5 association feeds relay.Datagrams, which asks the per-session routing cache for an outbound and sends through session.Session; the session holds one datagram per outbound, such as freedom's nat.Datagram. The router is shared; another client has its own session and NAT.](docs/assets/diagrams/udp-session-structure.excalidraw.svg)](docs/assets/diagrams/udp-session-structure.excalidraw.svg)

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
func NewBaseStreamTransportModule() spec.BaseStreamTransport
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
before publishing to GitHub Pages. See `docs/contributing.md` for how to edit the
pages and the diagrams.

## Licence

[Mozilla Public License 2.0](LICENSE).
