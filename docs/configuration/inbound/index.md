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

      "sniffing": {},
      "fallback": {},

      "handshake_timeout": "10s",
      "tcp_idle_timeout": "5m",
      "udp_session_idle_timeout": "5m",
      "max_connections": 0
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
run once per accepted connection, inside `handshake_timeout`.

### `protocol`

<span class="cfg cfg-type">module</span>
<span class="cfg cfg-req">Required</span>

`{ "type": ..., "settings": {...} }`, one of the [protocols](#protocols) above.

### `sniffing`

<span class="cfg cfg-type">object</span>
<span class="cfg cfg-def">Default: off</span>
<span class="cfg cfg-note">TCP only</span>

Recognise the protocol at the start of each connection. See
[Sniffing](../sniffing.md).

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

### `handshake_timeout`

<span class="cfg cfg-type">duration</span>
<span class="cfg cfg-def">Default: <code>10s</code></span>

How long a client has to finish its handshakes: every overlay's (TLS,
WebSocket upgrade) and then the proxy protocol's, together. Negative disables.

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

### `max_connections`

<span class="cfg cfg-type">integer</span>
<span class="cfg cfg-def">Default: <code>0</code>, no limit</span>
<span class="cfg cfg-note">TCP only</span>

How many connections are held at once. One more is closed as soon as it is
accepted, before any handshake; refusals are logged at most once per 10 seconds.

</div>
