# Client address behind a proxy

A server behind a CDN or a reverse proxy sees the proxy's address on every
connection. The proxy knows the real client and passes it on, in a header it
adds to the request. `ws` and `httpupgrade` read that header with their
`original_source_ip` setting, and routing (`source_ip`) and logs then see the
client. For a load balancer that cannot read HTTP, use
[`proxy_protocol`](proxy_protocol.md). Both write the same field, the
connection's source address.

This page is the format of `original_source_ip`, an inbound setting of
[`ws`](ws.md#original_source_ip) and [`httpupgrade`](httpupgrade.md#original_source_ip).

## Structure

```json
{ "type": "ws", "settings": {
    "path": "/chat",
    "original_source_ip": {
      "headers": ["X-Forwarded-For"],
      "verification": {
        "trusted_proxy_addresses": ["127.0.0.1"],
        "proof_header": { "name": "X-Edge-Secret", "value": "a-long-random-value" }
      }
    }
} }
```

Every key is optional. Without the block the feature is **on** and reads
`X-Forwarded-For` from any peer, as Xray and sing-box do.

<div class="cfg-fields" markdown>

### `disabled`

<span class="cfg cfg-type">boolean</span>
<span class="cfg cfg-def">Default: <code>false</code></span>

`true` turns the feature off: the header is never read and the source stays the
address of the layer below. Use it on a server with no proxy in front.

### `headers`

<span class="cfg cfg-type">list of strings</span>
<span class="cfg cfg-def">Default: <code>["X-Forwarded-For"]</code></span>

Header names, in order. The first one the request carries is read. If its value
is not an address, the whole header is ignored; the next name is not tried, so a
client cannot choose which header counts. An empty list is refused: use
`disabled`.

Only `X-Forwarded-For` is read by default, as in Xray and sing-box, because
every extra header is one more a client can set on a server with no CDN in
front. Others are safe only when your proxy overwrites them:

| Header | Set by | Safe when |
| --- | --- | --- |
| `CF-Connecting-IP` | Cloudflare | Cloudflare is in front |
| `True-Client-IP` | Akamai, Cloudflare Enterprise | that CDN is in front |
| `Fastly-Client-IP` | Fastly | Fastly is in front |
| `X-Real-IP` | nginx, when configured with `proxy_set_header X-Real-IP $remote_addr` | your nginx sets it |

`Forwarded` (RFC 7239) has a different syntax and is not read.

### `verification`

<span class="cfg cfg-type">object</span>
<span class="cfg cfg-def">Default: absent, nothing is checked</span>

How far to believe the header. If both parts are set, both must hold; a request
that fails is treated as if it had no header, and the connection goes on with the
address of the layer below.

| Field | Meaning |
| --- | --- |
| `trusted_proxy_addresses` | Addresses (`"10.0.0.2"`) and ranges (`"104.16.0.0/13"`). The address below this layer must be in the list, and listed addresses are skipped when the header is read from the right. |
| `proof_header` | `{ "name": ..., "value": ... }`: a header your proxy adds. The request must carry it with that value. An **empty** `value` means the header only has to be present, which is weaker: anyone who learns the name passes. The name may not also be in `headers`. |

</div>

## Which address it takes

A proxy appends the address it saw to the header, and a client can write
entries at the start, so the header is read **from the right**:

- With `trusted_proxy_addresses`, the client is the right-most address that is
  not listed. If every entry is listed, the left-most.
- Without it, the right-most entry. That is the client behind one proxy, which is
  what a standard nginx or a CDN sends.
- A value that is not an address (an empty entry, `unknown`, a zone), or no
  header, means the header is ignored.
- Repeated header lines are one list. IPv4-mapped IPv6 is read as IPv4.
- The port is the entry's when it carries one (`1.2.3.4:5678`, `[::1]:80`),
  otherwise `0`. A `source_port` rule does not match a source whose port is `0`.

Xray and sing-box take the **left-most** entry. A user who sends their own
`X-Forwarded-For: 1.2.3.4` through a proxy that appends gets `1.2.3.4, <real>`,
so those cores believe the forged address. Here it is ignored. Behind two
proxies the right-most entry is the first proxy, so list them.

## Setups

**nginx on the same machine,** forwarding to Apranik on loopback. Nothing to
set; the default works. To also ignore a header that does not come from nginx:

```json
"original_source_ip": { "verification": { "trusted_proxy_addresses": ["127.0.0.1"] } }
```

**Cloudflare directly.** The default works: Cloudflare appends the client to
`X-Forwarded-For`. For a header a client cannot forge, read Cloudflare's own and
verify with a secret your Cloudflare rule adds:

```json
"original_source_ip": {
  "headers": ["CF-Connecting-IP"],
  "verification": { "proof_header": { "name": "X-Edge-Secret", "value": "a-long-random-value" } }
}
```

**A CDN, then nginx, then Apranik.** The header reaches Apranik as
`client, cdn-edge`. List both the nginx address and the CDN's ranges:

```json
"original_source_ip": { "verification": { "trusted_proxy_addresses": ["10.0.0.2", "104.16.0.0/13"] } }
```

**A load balancer that sends a PROXY header, then a CDN's header.** Put
[`proxy_protocol`](proxy_protocol.md) directly above `tcp` and `ws` above it.
`proxy_protocol` reports the CDN edge as the source, and `ws` checks that address
against its own list, which is the CDN's ranges, not the load balancer's.

Behind Docker, nginx in another container connects from a bridge address, not
loopback, so it must be listed.

## Warnings

An inbound that listens on any address but loopback, with the feature on and no
`verification`, warns that any client that can reach the port can choose its own
source address. A loopback listen address is safe: only a proxy on the machine
can connect. The warning goes away with `verification`, or with `disabled`.
`apranik check` also warns when `trusted_proxy_addresses` covers every address,
and when `disabled` is set together with a `verification`.

An ignored header is silent: nothing says a request failed verification. If the
source shows a proxy's address, check the list, or run with the log level at
`debug`: a replaced source is logged there.
