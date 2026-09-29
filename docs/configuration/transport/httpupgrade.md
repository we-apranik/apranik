# HTTPUpgrade

An overlay stream transport: the HTTP/1.1 upgrade handshake WebSocket starts
with, then the payload as raw bytes, with no WebSocket framing. Like
[`ws`](ws.md) it passes CDNs and HTTP reverse proxies, at no cost after the
handshake, but only through ones that relay an upgraded connection without
checking its frames (Cloudflare does, in practice). Compatible with Xray's and
sing-box's `httpupgrade`; see [how it differs from `ws`](index.md#choosing-a-stack).

## Structure

=== "Inbound"

    ```json
    {
      "transport": [
        { "type": "tcp" },
        { "type": "tls", "settings": { "certificates": [{ "certificate_file": "cert.pem", "key_file": "key.pem" }] } },
        { "type": "httpupgrade", "settings": { "path": "/up", "host": "cdn.example.com" } }
      ]
    }
    ```

=== "Outbound"

    ```json
    {
      "server": "cdn.example.com",
      "server_port": 443,
      "transport": [
        { "type": "tcp" },
        { "type": "tls", "settings": { "server_name": "cdn.example.com", "alpn": ["http/1.1"] } },
        { "type": "httpupgrade", "settings": { "path": "/up" } }
      ]
    }
    ```

<div class="cfg-fields" markdown>

## Both

### `path`

<span class="cfg cfg-type">string</span>
<span class="cfg cfg-def">Default: <code>/</code></span>

The request path. A server compares it without the query string, so an Xray
client's `/up?ed=2048` matches `/up`. Any other path gets a `404`.

### `host`

<span class="cfg cfg-type">string</span>

Outbound: the `Host` header, which a CDN routes on; default the server's
address. Inbound: if set, the `Host` a request must carry (case and port
ignored); any other gets a `400`. Unset accepts any.

## Outbound

### `headers`

<span class="cfg cfg-type">object of strings</span>

Extra request headers. `Host`, `Upgrade` and `Connection` are written by the
module and refused here. Without a `User-Agent`, a browser's is sent.

### `pipeline`

<span class="cfg cfg-type">boolean</span>
<span class="cfg cfg-def">Default: <code>false</code></span>

Send the payload right after the request instead of waiting for the server's
`101`, saving a round trip on every new connection. This is what Xray's `?ed=`
path setting does for HTTPUpgrade. Apranik's server keeps such early bytes;
Xray's and sing-box's servers, as their source reads, can drop bytes that
arrive together with the request, which would lose the first payload. No loss
was seen against Xray in testing, but the risk is why it is off by default.

## Inbound

### `accept_websocket_key`

<span class="cfg cfg-type">boolean</span>
<span class="cfg cfg-def">Default: <code>true</code></span>

Accept a request carrying `Sec-WebSocket-Key`, as Xray does. `false` answers
it with a `404`, as sing-box does.

</div>

A request that is not a `GET` upgrade to `websocket` gets a `404`, a malformed
one a `400`, as sing-box answers. `Connection: keep-alive, Upgrade` is
accepted. Half-close passes through to the layer below. Behind a CDN the
inbound sees the CDN's address as the client's; `X-Forwarded-For` is not read
yet.

Its upgrade request is HTTP/1.1, so a [`tls`](tls.md) below it must offer only
`http/1.1`: set its `alpn` to `["http/1.1"]`, as the outbound above does.
`apranik check` refuses a `tls` whose list includes anything else
([why](tls.md#alpn)).
