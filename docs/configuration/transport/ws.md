# WebSocket

An overlay: WebSocket (RFC 6455) over HTTP/1.1, to pass through CDNs and HTTP
reverse proxies. Compatible with Xray's and sing-box's, early data included. A
[`tls`](tls.md) below it must set `"alpn": ["http/1.1"]`.

## Structure

:::tabs
== Inbound

```json
{
  "type": "ws",
  "settings": {
    "path": "/chat",
    "host": "cdn.example.com",
    "original_source_ip": { "headers": ["X-Forwarded-For"] }
  }
}
```

== Outbound

```json
{
  "type": "ws",
  "settings": {
    "path": "/chat",
    "host": "cdn.example.com",
    "headers": { "User-Agent": "Mozilla/5.0" },
    "early_data": { "max_bytes": 2048 }
  }
}
```

:::

<div class="cfg-fields">

## Fields

### `path`

<span class="cfg cfg-type">string</span>
<span class="cfg cfg-def">Default: <code>/</code></span>

The request path. A server ignores the query string and answers any other path
with `404`.

### `host`

<span class="cfg cfg-type">string</span>

Outbound: the `Host` header a CDN routes on; default the outbound's `server`.
Inbound: the only `Host` accepted; default any.

### `early_data`

<span class="cfg cfg-type">object</span>

Sends the start of the first payload inside the upgrade request, saving a round
trip.

- `max_bytes` (required, 1 to 16384): how many bytes are sent, or accepted. Xray's
  `?ed=2048` is `2048`.
- `location`: `header` (default) or `path`. `path` is sing-box's default; an Xray
  server answers it with `404`.
- `header_name`: default `Sec-WebSocket-Protocol`, which is Xray's.

An inbound without `early_data` still accepts it in `Sec-WebSocket-Protocol`.

### `headers`

<span class="cfg cfg-type">object of strings</span>
<span class="cfg cfg-note">outbound</span>

Extra request headers. Without a `User-Agent`, a browser's is sent.

### `original_source_ip`

<span class="cfg cfg-type">object</span>
<span class="cfg cfg-note">inbound</span>

Reads the client's address from a header the CDN adds. See
[Client address behind a proxy](original_source_ip.md).

### `handshake_timeout`

<span class="cfg cfg-type">duration</span>
<span class="cfg cfg-def">Default: <code>10s</code></span>

How long the upgrade may take. With early data it counts from the first write.

</div>

There is no half-close: a Close frame ends both directions.
