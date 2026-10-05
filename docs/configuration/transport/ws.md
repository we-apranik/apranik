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
        { "type": "tls", "settings": { "server_name": "cdn.example.com", "alpn": ["http/1.1"] } },
        {
          "type": "ws",
          "settings": {
            "path": "/chat",
            "headers": { "User-Agent": "Mozilla/5.0 ..." },
            "early_data": { "max_bytes": 2048 }
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

### `early_data`

<span class="cfg cfg-type">object</span>
<span class="cfg cfg-def">Default: absent</span>

Early data puts the start of the first payload in the upgrade request, base64,
so the server can begin without waiting a round trip; the rest follows as
frames. The outbound then sends its handshake with the first payload rather
than when the connection opens.

| Field | Meaning |
| --- | --- |
| `max_bytes` | Required, 1 to 16384. Outbound: how many bytes of the first payload go in the handshake. Inbound: the most it accepts; more gets a `400`. Xray's `?ed=2048` and sing-box's `max_early_data: 2048` correspond to 2048. |
| `location` | `"header"` (default) or `"path"`. |
| `header_name` | Only with `"header"`. Default `Sec-WebSocket-Protocol`. |

What each case does:

| Config | Outbound | Inbound |
| --- | --- | --- |
| absent | No early data; the handshake runs when the connection opens. Works with any server. | Accepts early data in `Sec-WebSocket-Protocol` (up to 16384 bytes) and echoes the header, so an Xray client works. The path must match exactly. |
| `{"max_bytes": 2048}` | Early data in `Sec-WebSocket-Protocol`: Xray's way, and sing-box's with `early_data_header_name: "Sec-WebSocket-Protocol"`. | The same header, up to 2048 bytes. |
| `"header_name": "X-Early"` | Early data in that header. Only a server set to the same name reads it; an Xray server ignores it and the connection fails. | Reads that header instead. The header is echoed only when it is `Sec-WebSocket-Protocol`. |
| `"location": "path"` | Early data appended to the path, before any query: `/chat` becomes `/chat<base64>`. sing-box's default. An Xray server answers `404`. | Also accepts `/chat<base64>` as `/chat`, and still accepts the header. A suffix that is not base64 gets a `400`. |

`header_name` with `"path"` is refused, and so is a `headers` entry for the
header that carries early data.

### `host`

<span class="cfg cfg-type">string</span>

Outbound: the `Host` header, which a CDN routes on; default the server's
address. It may differ from the TLS `server_name` and from the address dialed.
Inbound: if set, the `Host` a request must carry (case and port ignored); any
other gets a `400`. Unset accepts any.

### `handshake_timeout`

<span class="cfg cfg-type">duration</span>
<span class="cfg cfg-def">Default: <code>10s</code></span>

How long the upgrade handshake may take. Outbound with `early_data`, it is
counted from the first write, which sends the handshake; a deadline the caller
set on the connection still applies if it is earlier, and stays in force
afterwards.

## Inbound

### `original_source_ip`

<span class="cfg cfg-type">object</span>
<span class="cfg cfg-def">Default: on, reads <code>X-Forwarded-For</code> from any peer</span>

Read the client's IP from a header a proxy in front added, so routing and logs
see the client and not the CDN. Its format, the setups for nginx and Cloudflare,
and how it differs from Xray and sing-box are in
[Client address behind a proxy](original_source_ip.md).

## Outbound

### `headers`

<span class="cfg cfg-type">object of strings</span>

Extra request headers. `Host`, `Upgrade`, `Connection` and `Sec-WebSocket-*`
are written by the module and refused here. Without a `User-Agent`, a browser's
is sent.

</div>

Every write is one binary frame (split above 32 KiB). Pings are answered. A
WebSocket Close ends both directions, so there is no half-close. Behind a CDN
the inbound sees the CDN's address as the client's; `X-Forwarded-For` is not
read yet.

Its upgrade request is HTTP/1.1, so a [`tls`](tls.md) below it must offer only
`http/1.1`: set its `alpn` to `["http/1.1"]`, as the outbound above does.
`apranik check` refuses a `tls` whose list includes anything else
([why](tls.md#alpn)).
