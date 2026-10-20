# DNS over TLS

A `dot` DNS server is [`tcp`](tcp.md)'s framing inside TLS (RFC 7858): one connection reused, queries pipelined. Port `853` by default.

## Structure

<div class="cfg-structure">

```json
{
  "tag": "cloudflare",
  "type": "dot",
  "settings": { "server": "1.1.1.1", "timeout": "5s" },
  "tls": { "server_name": "one.one.one.one" },
  "outbound": "direct"
}
```

</div>

<div class="cfg-fields">

## Fields

### `settings.server`

<span class="cfg cfg-type">string</span>
<span class="cfg cfg-req">Required</span>

An IP or hostname with an optional port: `"1.1.1.1"`, `"dns.google:853"`.

### `settings.timeout`

<span class="cfg cfg-type">duration</span>
<span class="cfg cfg-def">Default: <code>5s</code></span>

How long one query may take, including reaching the server.

### `tls`

<span class="cfg cfg-type">object</span>

[`tls` transport settings](../transport/tls.md) for the TLS to the DNS server. Set `server_name` when `server` is an IP.

</div>

> [!TIP]
> To hide the ClientHello from the network, send the queries through an `outbound` with [`fragment`](../transport/fragment.md), or through a proxy.

The common fields (`tag`, `outbound`, `ip_versions`, `dns_server`) are on [DNS server](server.md).
