# DNS server

One upstream the [resolver](index.md) may ask, in `dns.servers`.

## Structure

<div class="cfg-structure">

```json
{
  "tag": "cloudflare",
  "type": "dot",
  "settings": { "server": "1.1.1.1", "timeout": "5s" },
  "tls": { "server_name": "one.one.one.one" },
  "outbound": "direct",
  "ip_versions": "ipv4_only",
  "dns_server": "os"
}
```

</div>

<div class="cfg-fields">

## Fields

### `tag`

<span class="cfg cfg-type">string</span>
<span class="cfg cfg-req">Required</span>

A name unique among DNS servers, used by rules, `default_server`, `resolve` and
`dns_server`.

### `type`

<span class="cfg cfg-type">string</span>
<span class="cfg cfg-req">Required</span>

| Type | Protocol | Default port |
| --- | --- | --- |
| [`udp`](udp.md) | DNS over UDP | `53` |
| [`tcp`](tcp.md) | DNS over TCP | `53` |
| [`dot`](dot.md) | DNS over TLS | `853` |
| [`doh`](doh.md) | DNS over HTTPS, HTTP/1.1 | `443` |
| [`system`](system.md) | the operating system's resolver | – |

### `settings`

<span class="cfg cfg-type">object</span>

The type's own settings: `server` and `timeout` for every type, `path` and `host`
for `doh`. See the page of each type.

### `outbound`

<span class="cfg cfg-type">string</span>
<span class="cfg cfg-def">Default: a plain freedom</span>

The outbound the queries go through: a freedom with
[`fragment`](../transport/fragment.md), or the proxy, so the network sees no DNS.
`udp` needs one that carries UDP.

### `ip_versions`

<span class="cfg cfg-type">string</span>
<span class="cfg cfg-def">Default: the section's</span>

[`ip_versions`](index.md#ip_versions) for this server.

### `dns_server`

<span class="cfg cfg-type">string</span>
<span class="cfg cfg-def">Default: <code>default_server</code></span>

The DNS server that resolves this server's hostname.

</div>
