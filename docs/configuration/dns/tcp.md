# DNS over TCP

A `tcp` DNS server sends each query behind a two-byte length (RFC 7766). One connection is reused and queries are pipelined on it; it closes after 30 seconds without an answer. Port `53` by default.

## Structure

<div class="cfg-structure">

```json
{
  "tag": "plain-tcp",
  "type": "tcp",
  "settings": { "server": "9.9.9.9", "timeout": "5s" },
  "outbound": "proxy"
}
```

</div>

<div class="cfg-fields">

## Fields

### `settings.server`

<span class="cfg cfg-type">string</span>
<span class="cfg cfg-req">Required</span>

An IP or hostname with an optional port: `"9.9.9.9"`, `"dns.example:53"`.

### `settings.timeout`

<span class="cfg cfg-type">duration</span>
<span class="cfg cfg-def">Default: <code>5s</code></span>

How long one query may take, including reaching the server.

</div>

> [!NOTE]
> Nothing is encrypted. Through a proxy `outbound` the network sees no DNS, but the proxy's exit does; use [`dot`](dot.md) or [`doh`](doh.md) to hide it from the path to the server.

The common fields (`tag`, `outbound`, `ip_versions`, `dns_server`) are on [DNS server](server.md).
