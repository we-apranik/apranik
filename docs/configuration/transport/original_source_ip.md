# Client address behind a proxy

Behind a CDN or reverse proxy, every connection comes from the proxy. The proxy
names the real client in a header, and the `original_source_ip` key of a
[`ws`](ws.md) or [`httpupgrade`](httpupgrade.md) inbound reads it, so routing
(`source_ip`) and logs see the client. For a load balancer that does not speak
HTTP, use [`proxy_protocol`](proxy_protocol.md).

Without the key, `X-Forwarded-For` is read from any peer, as Xray and sing-box do.

## Structure

<div class="cfg-structure" markdown>

```json
{
  "type": "ws",
  "settings": {
    "path": "/chat",
    "original_source_ip": {
      "disabled": false,
      "headers": ["X-Forwarded-For"],
      "verification": {
        "trusted_proxy_addresses": ["127.0.0.1"],
        "proof_header": {
          "name": "X-Edge-Secret",
          "value": "a-long-random-value"
        }
      }
    }
  }
}
```

</div>

<div class="cfg-fields" markdown>

## Fields

### `disabled`

<span class="cfg cfg-type">boolean</span>
<span class="cfg cfg-def">Default: <code>false</code></span>

Never read the header. Use it when nothing sits in front.

### `headers`

<span class="cfg cfg-type">list of strings</span>
<span class="cfg cfg-def">Default: <code>["X-Forwarded-For"]</code></span>

Header names; the first one present is read. Add another only if your proxy always
overwrites it: `CF-Connecting-IP` (Cloudflare), `True-Client-IP` (Akamai),
`Fastly-Client-IP` (Fastly), `X-Real-IP` (nginx).

### `verification.trusted_proxy_addresses`

<span class="cfg cfg-type">list of IPs or CIDRs</span>

The connection must come from one of these, or the header is ignored. Listed
addresses in the header are skipped.

### `verification.proof_header`

<span class="cfg cfg-type">object</span>

A `name` and `value` your proxy adds; a request without them has its header
ignored. An empty `value` only requires the header to exist.

</div>

## Which address is taken

The header is read from the right, because a proxy appends and a client can only
prepend. The client is the right-most address that is not a trusted proxy. Xray
and sing-box read the left-most address, which a client can forge (not tested by running them).

## Setups

| In front | Setting |
| --- | --- |
| nginx on the same machine | `"verification": { "trusted_proxy_addresses": ["127.0.0.1"] }` |
| Cloudflare | `"headers": ["CF-Connecting-IP"]`, with a `proof_header` your Cloudflare rule adds |
| a CDN, then nginx | list nginx's address and the CDN's ranges in `trusted_proxy_addresses` |
| nginx in Docker | list the bridge address, not loopback |

Without `verification` on a public address, `check` warns that any client can
choose its own source address.
