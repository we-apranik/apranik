# DNS server

One upstream the [resolver](index.md) may ask, in `dns.servers`.

## Structure

<div class="cfg-structure" markdown>

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

<div class="cfg-fields" markdown>

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
| `udp` | DNS over UDP | `53` |
| `tcp` | DNS over TCP | `53` |
| `dot` | DNS over TLS | `853` |
| `doh` | DNS over HTTPS, HTTP/1.1 | `443` |
| `system` | the operating system's resolver | – |

`system` takes only `tag`, `type` and `settings.timeout`.

### `settings.server`

<span class="cfg cfg-type">string</span>
<span class="cfg cfg-req">Required</span>

An IP or hostname with an optional port: `"8.8.8.8"`, `"dns.google:853"`,
`"[2001:4860:4860::8888]"`. Give `udp` an IP.

### `settings.timeout`

<span class="cfg cfg-type">duration</span>
<span class="cfg cfg-def">Default: <code>5s</code></span>

How long one query may take, including reaching the server.

### `settings.path`

<span class="cfg cfg-type">string</span>
<span class="cfg cfg-def">Default: <code>/dns-query</code></span>
<span class="cfg cfg-note">doh</span>

The request path.

### `settings.host`

<span class="cfg cfg-type">string</span>
<span class="cfg cfg-def">Default: the server's host</span>
<span class="cfg cfg-note">doh</span>

The `Host` header. Set it when `server` is an IP.

### `tls`

<span class="cfg cfg-type">object</span>
<span class="cfg cfg-note">dot, doh</span>

[`tls` transport settings](../transport/tls.md) for the TLS to the DNS server.
`doh` gets `"alpn": ["http/1.1"]` unless you write one.

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
