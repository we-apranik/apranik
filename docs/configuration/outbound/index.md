# Outbound

An outbound is how traffic leaves: through a proxy server, straight to the
destination, or nowhere.

> [!NOTE]
> There are no routing rules yet. Every inbound uses the **first** outbound, and
> `apranik run` logs a warning when more are configured.

## Structure

<div class="cfg-structure" markdown>

```json
{
  "outbounds": [
    {
      "tag": "upstream",
      "server": "203.0.113.5",
      "server_port": 1080,
      "transport": [{ "type": "tcp" }],
      "protocol": { "type": "socks5", "settings": {} },

      "dial_timeout": "10s",
      "handshake_timeout": "10s"
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
| `block` | [Block](block.md) | nothing: refuses everything |

`freedom` and `block` are special protocols built by core. They reach no proxy
server, so they take no `server`, `server_port`, `transport` or
`handshake_timeout`.

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
when it dials.

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
transport first.

### `dial_timeout`

<span class="cfg cfg-type">duration</span>
<span class="cfg cfg-def">Default: <code>10s</code></span>

How long reaching the server may take (for `freedom`, the destination). Negative
disables. Not for `block`.

### `handshake_timeout`

<span class="cfg cfg-type">duration</span>
<span class="cfg cfg-def">Default: <code>10s</code></span>
<span class="cfg cfg-note">proxy protocols only</span>

How long the proxy handshake with the server may take. Negative disables.

</div>
