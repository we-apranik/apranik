# Outbound

An outbound is how traffic leaves: through a proxy server, or straight to the
destination. Which outbound serves a request is decided by
[routing](../routing/index.md); without a `routing` section, every request takes the
**first** outbound. Blocking is a [routing action](../routing/index.md#block), not
an outbound.

## Structure

<div class="cfg-structure" markdown>

```json
{
  "outbounds": [
    {
      "tag": "upstream",
      "server": "203.0.113.5",
      "server_port": 1080,
      "transport": [{ "type": "tcp", "settings": { "dial_timeout": "10s" } }],
      "protocol": { "type": "socks5", "settings": { "handshake_timeout": "10s" } }
    }
  ]
}
```

</div>

## Protocols

| Type | Format | Carries |
| --- | --- | --- |
| `socks5` | [SOCKS5](../protocol/socks5.md) | TCP |
| `http` | [HTTP](../protocol/http.md) | TCP |
| `trojan` | [Trojan](../protocol/trojan.md) | TCP, and UDP framed in the stream |
| `vless` | [VLESS](../protocol/vless.md) | TCP, and UDP framed in the stream, plain or as XUDP |
| `vless` | [VLESS](../protocol/vless.md) | TCP, and UDP framed in the stream, plain or as XUDP |
| `freedom` | [Freedom](freedom.md) | TCP and UDP, straight to each destination |
| `urltest` | [Group](group.md) | what every member carries; picks the fastest member |
| `fallback` | [Group](group.md) | what every member carries; picks the first member that works |

`freedom` is a special protocol built by core. It reaches no proxy server, so
it takes no `server` or `server_port`, and its
`transport` holds only [`tcp` and transports that need no server
side](freedom.md#transport).

`urltest` and `fallback` are [groups](group.md), also built by core: an outbound that
picks one of other outbounds for each connection. They take none of `server`,
`server_port`, `transport` or `dns_server`.

<div class="cfg-fields" markdown>

## Fields

### `tag`

<span class="cfg cfg-type">string</span>
<span class="cfg cfg-req">Required</span>

A name unique among outbounds. It appears in logs.

### `protocol`

<span class="cfg cfg-type">module</span>
<span class="cfg cfg-req">Required</span>

`{ "type": ..., "settings": {...} }`, one of the [protocols](#protocols) above.

### `server`

<span class="cfg cfg-type">address</span>
<span class="cfg cfg-req">Required</span>
<span class="cfg cfg-note">proxy protocols only</span>

The proxy server: an IP or a domain. A domain is resolved by the base transport
when it dials. Overlays that need a name use it too: [`tls`](../transport/tls.md)
sends a domain `server` as the SNI unless `server_name` is set, and
[`ws`](../transport/ws.md) and [`httpupgrade`](../transport/httpupgrade.md) send
it as the `Host` unless `host` is set.

### `server_port`

<span class="cfg cfg-type">integer</span>
<span class="cfg cfg-req">Required</span>
<span class="cfg cfg-note">proxy protocols only</span>

The proxy server's port.

### `transport`

<span class="cfg cfg-type">list of modules</span>
<span class="cfg cfg-req">Required</span>
<span class="cfg cfg-note">proxy protocols only</span>

The [transport stack](../transport/index.md) used to reach the server, base
transport first, then any overlays, for example
`[{ "type": "tcp" }, { "type": "tls" }]`.

### `muxcool`

<span class="cfg cfg-type">object</span>
<span class="cfg cfg-note">proxy protocols that can carry Mux.Cool only (`vless`)</span>

How the outbound uses [Mux.Cool](../../mux-cool.md). Left out, it means what the clients of Xray
and sing-box do: UDP is sent as XUDP, and TCP is not multiplexed. Core reads it, and a protocol
that cannot carry Mux.Cool refuses it at `apranik check`. The whole shape is written below; the
keys of what is not built are refused when set, with a message that says so. An
[inbound](../inbound/index.md#muxcool) takes the same key for what its server accepts.

```json
"muxcool": {
  "tcp": { "enabled": false, "concurrency": 8, "max_total": 128 },
  "udp": {
    "enabled": true,
    "xudp": { "enabled": true, "global_id": { "enabled": true } },
    "concurrency": 8
  }
}
```

| Key | Meaning | Default | Status |
| --- | --- | --- | --- |
| `udp.enabled` | Send UDP through Mux.Cool. `false` sends the protocol's plain UDP. | `true` | Works |
| `udp.xudp.enabled` | One sub-connection carries every destination of a UDP session. `false` would be plain Mux.Cool UDP. | `true` | `true` works; `false` is refused |
| `udp.xudp.global_id.enabled` | Send the session's Global ID, so a server that supports it gives the session back after the stream is lost (below). | `true` | Works |
| `udp.xudp.global_id.hold_time` | How long a server holds a lost session. A server's key: refused here. | – | Refused on an outbound |
| `udp.concurrency` | UDP sessions sharing one main connection | – | Refused: each UDP session has its own main connection |
| `tcp.enabled` | Multiplex TCP connections | `false` | `true` is refused; `false` is accepted |
| `tcp.concurrency` | TCP connections per main connection (1 to 128) | `8` | Refused: TCP over Mux.Cool is not built |
| `tcp.max_total` | Sub-connections ever carried by one main connection | `128` | Refused: TCP over Mux.Cool is not built |

A written zero is refused. `xudp` used to be a plain `true` or `false`; it is an object now, and a bare
boolean is an error.

#### The Global ID

When the stream that carries a UDP session is lost (a phone moves from Wi-Fi to mobile data), the
client opens a new one. With the Global ID the client names the session in the first frame, from an
ID it works out from the session's source address and a secret key it makes at start, and a server
that remembers the session gives it back: the destination sees the same source port, so a call or a
game goes on. Xray's server does this, and so does an Apranik [inbound](../inbound/index.md#muxcool).
sing-box ignores the ID. `global_id.enabled: false` sends none.

### `proxy_request_timing`

<span class="cfg cfg-type">object</span>
<span class="cfg cfg-note">proxy protocols whose client holds its request (`trojan`, `vless`)</span>

When the client sends the request it owes its proxy server. It holds the request until the
application's first bytes, so the two travel in one write and a connection never opens with a
small packet of a fixed length on its own, which a DPI could recognise. After `max_delay` it
sends the request alone. Core reads this key, and a protocol that does not hold its request
refuses it at `apranik check`.

```json
"proxy_request_timing": {
  "max_delay": "400ms",
  "skip": { "enabled": false, "ports": [21, 25, 110, 143, 587, 3306] }
}
```

| Key | Meaning | Default |
| --- | --- | --- |
| `max_delay` | The longest the request waits for the application's first bytes. A longer wait delays the first byte of server-first protocols; a shorter one sends the request alone more often. | `400ms` |
| `skip.enabled` | Send the request at once, without waiting, for the ports in `skip.ports`. | `false` |
| `skip.ports` | The destination ports that send at once. Writing it replaces the built-in list; an empty list, a number outside 1 to 65535 and a duplicate are refused. Written while `skip.enabled` is not `true`, it only gets a warning. | `[21, 25, 110, 143, 587, 3306]` |

**What `skip` is for.** FTP, SMTP, POP3, IMAP and MySQL are server-first: the application sends
nothing until the server has spoken, and the server speaks only after it has the request. Without
`skip` these connections start `max_delay` late. With it, a connection to a listed port sends the
request at once.

**What it costs.** A request sent alone is a small packet of a fixed length at the start of every
such connection, the pattern the wait exists to avoid. So `skip` is off by default; turn it on only
for ports where that trade is acceptable. The built-in ports are server-first and none starts with
TLS; SSH (22) is left out because its client sends first, and so are 465, 993 and 995, which start
with a TLS handshake.

`skip` does not apply to UDP: a UDP session sends its request with its first datagram. A written
zero or negative `max_delay` is refused. An inbound has no `proxy_request_timing`: it has no request
to send.

</div>
