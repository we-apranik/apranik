# Inbound

An inbound accepts connections from clients: it listens on an address, runs a
[transport](../transport/index.md) stack, and speaks a proxy protocol.

## Structure

<div class="cfg-structure" markdown>

```json
{
  "inbounds": [
    {
      "tag": "socks-in",
      "listen": "127.0.0.1",
      "port": 1080,
      "network": "tcp",

      "transport": [{ "type": "tcp" }],
      "protocol": { "type": "socks5", "settings": {} },

      "fallback": {},

      "tcp_idle_timeout": "5m",
      "udp_session_idle_timeout": "5m",
      "udp_max_datagram_size": 8192,
      "udp_session_route_cache_idle_timeout": "1m",
      "udp_session_route_cache_max_entries": 1024,
      "udp_session_live_connections_idle_timeout": "1m",
      "udp_session_live_connections_max_entries": 1024,
      "max_tcp_connections": 0,
      "max_udp_sessions": 0
    }
  ]
}
```

</div>

## Protocols

| Type | Format | Carries |
| --- | --- | --- |
| `socks5` | [SOCKS5](../protocol/socks5.md) | TCP, and UDP through UDP ASSOCIATE |
| `http` | [HTTP](../protocol/http.md) | TCP |
| `trojan` | [Trojan](../protocol/trojan.md) | TCP, and UDP framed in the stream |
| `mixed` | [Mixed](mixed.md) | SOCKS5 and HTTP on one port |

<div class="cfg-fields" markdown>

## Fields

### `tag`

<span class="cfg cfg-type">string</span>
<span class="cfg cfg-req">Required</span>

A name unique among inbounds. It appears in logs.

### `listen`

<span class="cfg cfg-type">IP address</span>
<span class="cfg cfg-req">Required</span>

The address to listen on, such as `127.0.0.1` or `::`. There is no default, so
nothing is exposed on every interface because a field was left out. A domain is
refused.

### `port`

<span class="cfg cfg-type">integer</span>
<span class="cfg cfg-req">Required</span>

The port to listen on.

### `network`

<span class="cfg cfg-type">string</span>
<span class="cfg cfg-def">Default: <code>tcp</code></span>

`tcp` or `udp`. `udp` is read but refused when the inbound is built: no datagram
proxy protocol exists yet.

### `transport`

<span class="cfg cfg-type">list of modules</span>
<span class="cfg cfg-req">Required</span>

The [transport stack](../transport/index.md), base transport first, then any
overlays: `[{ "type": "tcp" }]`, or for example
`[{ "type": "tcp" }, { "type": "tls", ... }, { "type": "ws", ... }]`. Overlays
run once per accepted connection, in its own goroutine, each within the
`handshake_timeout` in its own settings.

### `protocol`

<span class="cfg cfg-type">module</span>
<span class="cfg cfg-req">Required</span>

`{ "type": ..., "settings": {...} }`, one of the [protocols](#protocols) above.

### `fallback`

<span class="cfg cfg-type">object</span>
<span class="cfg cfg-def">Default: none</span>
<span class="cfg cfg-note">TCP only</span>

```json
"fallback": { "server": "127.0.0.1", "server_port": 8080 }
```

Where a request the protocol refuses is sent instead of being closed: typically a
local web server, so a probe sees an ordinary site. Apranik replays the request
from its first byte over plain TCP. Connections that close, reset or stay silent
are closed, not forwarded. Today only [`trojan`](../protocol/trojan.md) refuses
into a fallback.

The fallback receives what the client sent inside the overlays: behind `tls` it
gets the decrypted HTTP request, not TLS records. A client that fails an overlay
handshake (not TLS at all, or a wrong WebSocket path, which gets a `404`) is
closed, not forwarded.

### `tcp_idle_timeout`

<span class="cfg cfg-type">duration</span>
<span class="cfg cfg-def">Default: <code>5m</code></span>

Closes a TCP connection silent in both directions for this long. Negative
disables.

### `udp_session_idle_timeout`

<span class="cfg cfg-type">duration</span>
<span class="cfg cfg-def">Default: <code>5m</code></span>

Ends a UDP session silent for this long. UDP has no close, so this must not be
negative: a session would never be released.

### `udp_max_datagram_size`

<span class="cfg cfg-type">integer, bytes</span>
<span class="cfg cfg-def">Default: <code>8192</code></span>

The largest UDP datagram a client may send and a reply may be. A bigger one is dropped and
counted (`too_large_from_client`, `too_large_from_outbound`), never cut short. It sets the
buffers a UDP session holds: a session needs about 31 KB with the default, against about
146 KB when buffers held the largest datagram UDP allows (65,507 bytes). A network carries
about 1,472 bytes without fragmenting, QUIC and WireGuard use about 1,400, and a DNS message
is at most 4,096, so the default has room to spare. Raise it only if a client really sends
larger datagrams over a loopback or jumbo-frame link. `0` means the default; `apranik check`
refuses more than 65,507 and warns below 512.

The same limit applies where a datagram enters or leaves other parts: set the SOCKS5
inbound's [`udp_max_datagram_size`](../protocol/socks5.md#udp_max_datagram_size) and
freedom's [`nat.max_datagram_size`](../outbound/freedom.md#natmax_datagram_size) to match.

### `udp_session_route_cache_idle_timeout`

<span class="cfg cfg-type">duration</span>
<span class="cfg cfg-def">Default: <code>1m</code></span>

A UDP session routes each destination it sends to once and remembers the decision in
its route cache. After this much silence from the client to one destination, the session
forgets that decision, and a later packet to it is routed again. Forgetting a decision
does not end the destination's [live entry](../stats.md#live-connections-and-online-users).
Must not be negative. `apranik check` warns when it is longer than `udp_session_idle_timeout`:
the session ends first. A very small value makes every session wake often and route almost
every packet; keep it above a second.

### `udp_session_route_cache_max_entries`

<span class="cfg cfg-type">integer</span>
<span class="cfg cfg-def">Default: <code>1024</code></span>

How many destinations one UDP session remembers a routing decision for. When it is full,
the destination used longest ago is forgotten. A forgotten destination is simply routed
again, so a destination is never allowed because of this limit, and it is still carried and
counted. It only costs a client that uses more destinations at once than this, such as a P2P
application, extra routing. Nothing is reserved up front. Must not be negative; `0` means
the default.

### `udp_session_live_connections_idle_timeout`

<span class="cfg cfg-type">duration</span>
<span class="cfg cfg-def">Default: <code>1m</code></span>
<span class="cfg cfg-note">only with <code>stats.live_connections</code></span>

A UDP destination's live entry ends after this much silence in either direction, from the
client or from the destination. Must not be negative; `apranik check` warns when it is
longer than `udp_session_idle_timeout`.

### `udp_session_live_connections_max_entries`

<span class="cfg cfg-type">integer</span>
<span class="cfg cfg-def">Default: <code>1024</code></span>
<span class="cfg cfg-note">only with <code>stats.live_connections</code></span>

How many destinations of one UDP session are listed as live at once, about 490 bytes each
(measured).
When it is full, the entry used longest ago, in either direction, ends. The destination is
still carried and counted. Must not be negative; `0` means the default.

### `max_tcp_connections`

<span class="cfg cfg-type">integer</span>
<span class="cfg cfg-def">Default: <code>0</code>, no limit</span>
<span class="cfg cfg-note">TCP only</span>

How many TCP connections are held at once. One more is closed as soon as it is
accepted, before any handshake; refusals are logged at most once per 10 seconds.
Must not be negative. Called `max_connections` before UDP sessions had their own limit;
the old name is refused with a pointer here.

### `max_udp_sessions`

<span class="cfg cfg-type">integer</span>
<span class="cfg cfg-def">Default: <code>0</code>, no limit</span>

How many UDP sessions are held at once: a SOCKS5 UDP association, or a UDP session
over a Trojan stream. A request for one more is refused (SOCKS5 answers "general
failure", Trojan closes the stream) and counted as `max_udp_sessions`; refusals are
logged at most once per 10 seconds. A UDP session also holds its TCP connection, so
it uses one place of each limit. Must not be negative.

There is no limit unless you set one. Set it on a server open to people you do not
trust, where one client could otherwise open sessions until memory or file descriptors
run out. Each session holds about 31 KB through the handler, about 48 KB with a SOCKS5
association and the NAT (measured), so 8,192 sessions is about 390 MB, before the
destinations' live entries.

</div>
