# Inbound

An inbound accepts connections: it listens on an address, runs a
[transport stack](../transport/index.md) and speaks a proxy protocol.

## Structure

<div class="cfg-structure">

```json
{
  "inbounds": [
    {
      "tag": "vless-in",
      "listen": "0.0.0.0",
      "port": 443,
      "network": "tcp",
      "transport": [{ "type": "tcp" }],
      "protocol": {
        "type": "vless",
        "settings": {
          "users": [
            { "name": "alice", "uuid": "b831381d-6324-4d53-ad4f-8cda48b30811" }
          ]
        }
      },
      "fallback": { "server": "127.0.0.1", "server_port": 8080 },
      "muxcool": {},
      "tcp_idle_timeout": "5m",
      "udp_session_idle_timeout": "5m",
      "udp_max_datagram_size": 8192,
      "udp_routing_cache_entry_idle_timeout": "1m",
      "udp_routing_cache_max_entries_per_session": 1024,
      "max_tcp_connections": 10000,
      "max_udp_sessions": 2000
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
| `vless` | [VLESS](../protocol/vless.md) | TCP, and UDP framed in the stream |

Built in, made by core rather than a module:

| Type | Format | Carries |
| --- | --- | --- |
| `mixed` | [Mixed](mixed.md) | SOCKS5 and HTTP on one port |

<div class="cfg-fields">

## Fields

### `tag`

<span class="cfg cfg-type">string</span>
<span class="cfg cfg-req">Required</span>

A name unique among inbounds, used in logs, routing and statistics.

### `listen`

<span class="cfg cfg-type">IP address</span>
<span class="cfg cfg-req">Required</span>

The address to listen on, such as `127.0.0.1` or `::`. There is no default, so
nothing is exposed by accident.

### `port`

<span class="cfg cfg-type">integer</span>
<span class="cfg cfg-req">Required</span>

The port to listen on.

### `network`

<span class="cfg cfg-type">string</span>
<span class="cfg cfg-def">Default: <code>tcp</code></span>

Only `tcp` is built. `udp` is refused until a datagram proxy protocol exists.

### `transport`

<span class="cfg cfg-type">list of transports</span>
<span class="cfg cfg-req">Required</span>

The [transport stack](../transport/index.md): a base transport, then overlays.

### `protocol`

<span class="cfg cfg-type">object</span>
<span class="cfg cfg-req">Required</span>

`type` and `settings` of one of the [protocols](#protocols).

### `fallback`

<span class="cfg cfg-type">object</span>
<span class="cfg cfg-note">trojan, vless</span>

Where a request the protocol refuses goes, usually a local web server, so a probe
sees an ordinary site. It is replayed from its first byte over plain TCP, as
decrypted by the overlays. Connections that fail an overlay handshake are closed.

### `muxcool`

<span class="cfg cfg-type">object</span>
<span class="cfg cfg-note">vless</span>

What the server accepts of Mux.Cool. See [Mux.Cool](../shared/muxcool.md).

### `tcp_idle_timeout`

<span class="cfg cfg-type">duration</span>
<span class="cfg cfg-def">Default: <code>5m</code></span>

Closes a TCP connection silent both ways for this long. Negative disables it.

### `udp_session_idle_timeout`

<span class="cfg cfg-type">duration</span>
<span class="cfg cfg-def">Default: <code>5m</code></span>

Ends a UDP session silent for this long.

### `udp_max_datagram_size`

<span class="cfg cfg-type">integer, bytes</span>
<span class="cfg cfg-def">Default: <code>8192</code></span>

The largest UDP datagram carried either way; a larger one is dropped and counted,
never cut. At most 65,507. Keep it equal to SOCKS5's
[`udp_max_datagram_size`](../protocol/socks5.md#udp_max_datagram_size) and
freedom's [`nat.max_datagram_size`](../outbound/freedom.md#natmax_datagram_size).

### `udp_routing_cache_entry_idle_timeout`

<span class="cfg cfg-type">duration</span>
<span class="cfg cfg-def">Default: <code>1m</code></span>

A UDP session routes each destination once and remembers the decision. After this
much silence toward a destination, the decision is forgotten and made again.

### `udp_routing_cache_max_entries_per_session`

<span class="cfg cfg-type">integer</span>
<span class="cfg cfg-def">Default: <code>1024</code></span>

How many routing decisions one UDP session remembers. The oldest is forgotten
first and simply routed again.

### `max_tcp_connections`

<span class="cfg cfg-type">integer</span>
<span class="cfg cfg-def">Default: no limit</span>

TCP connections held at once. One more is closed before any handshake.

### `max_udp_sessions`

<span class="cfg cfg-type">integer</span>
<span class="cfg cfg-def">Default: no limit</span>

UDP sessions held at once. Each also holds a TCP connection, and costs about
48 KB. Set it on a server open to people you do not trust.

</div>
