# DNS over HTTPS

A `doh` DNS server posts each query as `application/dns-message` over HTTP/1.1 inside TLS (RFC 8484). HTTP/2 is not spoken, so `tls` gets `"alpn": ["http/1.1"]` unless you write one. Port `443` by default.

## Structure

<div class="cfg-structure">

```json
{
  "tag": "google",
  "type": "doh",
  "settings": { "server": "8.8.8.8", "host": "dns.google", "path": "/dns-query" },
  "tls": { "server_name": "dns.google" },
  "outbound": "proxy"
}
```

</div>

<div class="cfg-fields">

## Fields

### `settings.server`

<span class="cfg cfg-type">string</span>
<span class="cfg cfg-req">Required</span>

An IP or hostname with an optional port: `"8.8.8.8"`, `"dns.google"`, `"[2001:4860:4860::8888]"`.

### `settings.path`

<span class="cfg cfg-type">string</span>
<span class="cfg cfg-def">Default: <code>/dns-query</code></span>

The request path. It must be absolute.

### `settings.host`

<span class="cfg cfg-type">string</span>
<span class="cfg cfg-def">Default: the server's host</span>

The `Host` header. Set it when `server` is an IP.

### `settings.timeout`

<span class="cfg cfg-type">duration</span>
<span class="cfg cfg-def">Default: <code>5s</code></span>

How long one query may take, including reaching the server.

### `tls`

<span class="cfg cfg-type">object</span>

[`tls` transport settings](../transport/tls.md) for the TLS to the DNS server.

</div>

The common fields (`tag`, `outbound`, `ip_versions`, `dns_server`) are on [DNS server](server.md).
