# WebSocket

An overlay stream transport: WebSocket (RFC 6455) over HTTP/1.1, on top of the
layer below it. Used to pass through CDNs and HTTP reverse proxies, usually
with [`tls`](tls.md) below it or at the CDN. Compatible with Xray's and
sing-box's WebSocket transports, early data included; see
[how it differs from `httpupgrade`](index.md#choosing-a-stack).

## Structure

=== "Inbound"

    ```json
    {
      "transport": [
        { "type": "tcp" },
        { "type": "tls", "settings": { "certificates": [{ "certificate_file": "cert.pem", "key_file": "key.pem" }] } },
        { "type": "ws", "settings": { "path": "/chat" } }
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
        { "type": "tls", "settings": { "server_name": "cdn.example.com" } },
        {
          "type": "ws",
          "settings": {
            "path": "/chat",
            "headers": { "User-Agent": "Mozilla/5.0 ..." },
            "max_early_data": 2048
          }
        }
      ]
    }
    ```

<div class="cfg-fields" markdown>

## Both

### `path`

<span class="cfg cfg-type">string</span>
<span class="cfg cfg-def">Default: <code>/</code></span>

The request path. A server compares it without the query string, so an Xray
client's `/chat?ed=2048` matches `/chat`. A request for any other path gets a
`404`; a malformed upgrade gets a `400`.

### `early_data_header_name`

<span class="cfg cfg-type">string</span>
<span class="cfg cfg-def">Default: <code>Sec-WebSocket-Protocol</code></span>

The header that carries early data, as Xray uses. A server always accepts early
data there and echoes the header. Set it to `""` for sing-box's path mode, where
early data is appended to the path; a server then accepts it only when
`max_early_data` is above 0.

### `host`

<span class="cfg cfg-type">string</span>

Outbound: the `Host` header, which a CDN routes on; default the server's
address. It may differ from the TLS `server_name` and from the address dialed.
Inbound: if set, the `Host` a request must carry (case and port ignored); any
other gets a `400`. Unset accepts any.

## Outbound

### `headers`

<span class="cfg cfg-type">object of strings</span>

Extra request headers. `Host`, `Upgrade`, `Connection` and `Sec-WebSocket-*`
are written by the module and refused here. Without a `User-Agent`, a browser's
is sent.

### `max_early_data`

<span class="cfg cfg-type">integer</span>
<span class="cfg cfg-def">Default: <code>0</code> (off)</span>

Up to this many bytes of the first payload travel in the handshake, saving a
round trip; the rest follow as frames. Xray's `?ed=2048` corresponds to 2048.
With it on, the handshake is sent with the first payload rather than when the
connection opens. At most 16384.

</div>

Every write is one binary frame (split above 32 KiB). Pings are answered. A
WebSocket Close ends both directions, so there is no half-close. Behind a CDN
the inbound sees the CDN's address as the client's; `X-Forwarded-For` is not
read yet.
