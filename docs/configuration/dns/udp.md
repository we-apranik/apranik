# DNS over UDP

A `udp` DNS server sends each query as one datagram (RFC 1035) and takes the answer that carries the query's ID (RFC 5452). Port `53` by default.

## Structure

<div class="cfg-structure">

```json
{
  "tag": "domestic",
  "type": "udp",
  "settings": { "server": "192.0.2.53", "timeout": "5s" },
  "outbound": "direct"
}
```

</div>

<div class="cfg-fields">

## Fields

### `settings.server`

<span class="cfg cfg-type">string</span>
<span class="cfg cfg-req">Required</span>

An IP or hostname with an optional port: `"8.8.8.8"`, `"[2001:4860:4860::8888]:53"`. Give an IP: a hostname needs `dns_server` to resolve it, and `check` warns.

### `settings.timeout`

<span class="cfg cfg-type">duration</span>
<span class="cfg cfg-def">Default: <code>5s</code></span>

How long one query may take, including reaching the server.

</div>

> [!NOTE]
> The `outbound` must carry UDP. An answer too large for one datagram is an error; there is no retry over TCP. Use [`tcp`](tcp.md), [`dot`](dot.md) or [`doh`](doh.md) for such servers.

The common fields (`tag`, `outbound`, `ip_versions`, `dns_server`) are on [DNS server](server.md).
