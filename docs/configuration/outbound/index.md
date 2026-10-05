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

</div>
