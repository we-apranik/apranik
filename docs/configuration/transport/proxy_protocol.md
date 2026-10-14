# PROXY protocol

An inbound overlay that reads the PROXY protocol header (HAProxy's, versions 1 and
2) a load balancer writes before the client's bytes, and makes the client it names
the connection's source. HAProxy (`send-proxy`), nginx (`proxy_protocol on`), AWS
load balancers and Cloudflare Spectrum send it. Unlike
[`original_source_ip`](original_source_ip.md), it works for any protocol.

## Structure

<div class="cfg-structure" markdown>

```json
{
  "type": "proxy_protocol",
  "settings": {
    "verification": { "trusted_proxy_addresses": ["10.0.0.2"] },
    "timeout": "1s"
  }
}
```

</div>

> [!IMPORTANT]
> Put it directly after `tcp`, before every other overlay. Nothing checks this.

<div class="cfg-fields" markdown>

## Fields

### `verification.trusted_proxy_addresses`

<span class="cfg cfg-type">list of IPs or CIDRs</span>
<span class="cfg cfg-def">Default: <code>["127.0.0.0/8", "::1"]</code></span>

The proxies allowed to connect. Every peer must send the header, and a peer outside
the list is refused.

### `timeout`

<span class="cfg cfg-type">duration</span>
<span class="cfg cfg-def">Default: <code>1s</code></span>

How long a peer may take to send the header.

</div>

Both versions are detected automatically. A header that names no client, such as a
health check's `LOCAL`, is accepted and the source stays the peer. A connection
without a valid header is closed and logged at `debug`.
