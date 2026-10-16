# Outbound

An outbound is how traffic leaves: through a proxy server, straight to the
destination, or through a group that picks another outbound.
[Routing](../routing/index.md) decides which outbound a request takes; without a
`routing` section, every request takes the first. Blocking is a
[routing action](../routing/action.md#block), not an outbound.

## Structure

<div class="cfg-structure">

```json
{
  "outbounds": [
    {
      "tag": "proxy",
      "server": "proxy.example.com",
      "server_port": 443,
      "transport": [{ "type": "tcp" }, { "type": "tls" }],
      "protocol": {
        "type": "vless",
        "settings": { "uuid": "b831381d-6324-4d53-ad4f-8cda48b30811" }
      },
      "dns_server": "os",
      "muxcool": {},
      "proxy_request_timing": { "max_delay": "400ms" }
    }
  ],
  "dns": {
    "servers": [{ "tag": "os", "type": "system" }],
    "default_server": "os"
  }
}
```

</div>

## Protocols

| Type | Format | Carries |
| --- | --- | --- |
| `socks5` | [SOCKS5](../protocol/socks5.md) | TCP |
| `http` | [HTTP](../protocol/http.md) | TCP |
| `trojan` | [Trojan](../protocol/trojan.md) | TCP and UDP |
| `vless` | [VLESS](../protocol/vless.md) | TCP and UDP |
| `freedom` | [Freedom](freedom.md) | TCP and UDP, straight to each destination |
| `urltest` | [URLTest](urltest.md) | what every member carries |
| `fallback` | [Fallback](fallback.md) | what every member carries |
| `selector` | [Selector](selector.md) | what every member carries |

`freedom` and the groups take only some of the keys below; their pages say which.

<div class="cfg-fields">

## Fields

### `tag`

<span class="cfg cfg-type">string</span>
<span class="cfg cfg-req">Required</span>

A name unique among outbounds, used by routing, DNS servers, groups and statistics.

### `server`

<span class="cfg cfg-type">address</span>
<span class="cfg cfg-req">Required</span>

The proxy server, an IP or a domain. `tls` sends a domain as its SNI, and `ws` and
`httpupgrade` as the `Host`, unless they set their own.

### `server_port`

<span class="cfg cfg-type">integer</span>
<span class="cfg cfg-req">Required</span>

The proxy server's port.

### `transport`

<span class="cfg cfg-type">list of transports</span>
<span class="cfg cfg-req">Required</span>

The [transport stack](../transport/index.md) that reaches the server.

### `protocol`

<span class="cfg cfg-type">object</span>
<span class="cfg cfg-req">Required</span>

`type` and `settings` of one of the [protocols](#protocols).

### `dns_server`

<span class="cfg cfg-type">string</span>
<span class="cfg cfg-def">Default: the DNS rules' choice</span>

The tag of the [DNS server](../dns/index.md#servers) that resolves `server` when it
is a domain.

### `muxcool`

<span class="cfg cfg-type">object</span>
<span class="cfg cfg-note">vless</span>

How the client uses Mux.Cool. See [Mux.Cool](../shared/muxcool.md).

### `proxy_request_timing`

<span class="cfg cfg-type">object</span>
<span class="cfg cfg-note">trojan, vless</span>

When the client sends its proxy request. See
[Proxy request timing](../shared/proxy-request-timing.md).

</div>
