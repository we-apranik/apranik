# TLS

An overlay: TLS 1.2 and 1.3 over the layer below. On an outbound it is the client
and checks the server's certificate; on an inbound it is the server and presents
one.

> [!NOTE]
> A client sends Chrome's ClientHello by default, through
> [uTLS](https://github.com/refraction-networking/utls), because Go's own is easy
> to single out. REALITY and ECH are not supported yet.

## Structure

=== "Inbound"

    ```json
    {
      "type": "tls",
      "settings": {
        "certificates": [
          {
            "certificate_file": "/etc/apranik/cert.pem",
            "key_file": "/etc/apranik/key.pem"
          }
        ],
        "alpn": ["http/1.1"]
      }
    }
    ```

=== "Outbound"

    ```json
    {
      "type": "tls",
      "settings": {
        "server_name": "proxy.example.com",
        "fingerprint": "chrome",
        "alpn": ["http/1.1"]
      }
    }
    ```

<div class="cfg-fields" markdown>

## Outbound fields

### `server_name`

<span class="cfg cfg-type">string</span>
<span class="cfg cfg-def">Default: the outbound's <code>server</code> domain</span>

The SNI, checked against the certificate. With an IP `server` and no name, no SNI
is sent and the certificate must list the IP.

### `fingerprint`

<span class="cfg cfg-type">string</span>
<span class="cfg cfg-def">Default: <code>chrome</code></span>

Whose ClientHello to send. The names are Xray's and sing-box's, so a share link's
`fp=` carries over.

| Value | Sends |
| --- | --- |
| `chrome`, `firefox`, `safari` | the newest version uTLS has (Chrome 133, Firefox 120, Safari 16.0) |
| `ios`, `android`, `edge`, `360`, `qq` | older presets, warned about |
| `random` | one of the above, chosen at start |
| `randomized` | a made-up ClientHello, fixed at start |
| `none` | Go's own |
| `hellochrome_131` and other uTLS names | that exact version |

`alpn` replaces the preset's ALPN list and nothing else. A server ignores this key.

### `insecure`

<span class="cfg cfg-type">boolean</span>
<span class="cfg cfg-def">Default: <code>false</code></span>

Accept any certificate, so anyone on the path can read the traffic. Warned about.

### `certificate_authorities`

<span class="cfg cfg-type">list of certificates</span>

Authorities trusted besides the system's, each with a `certificate_file` path or an
inline PEM `certificate`.

## Inbound fields

### `certificates`

<span class="cfg cfg-type">list of key pairs</span>
<span class="cfg cfg-req">Required</span>

Chains to present, each with its key: `certificate_file` or inline `certificate`,
and `key_file` or inline `key`. The one matching the client's SNI is used. They are
read at `run`, not at `check`.

## Fields for both

### `alpn`

<span class="cfg cfg-type">list of strings</span>

Application protocols in preference order. Under `ws` or `httpupgrade`, which speak
only HTTP/1.1, it must be `["http/1.1"]`: otherwise the far end may pick `h2` and
fail, so `check` refuses it.

### `min_version`, `max_version`

<span class="cfg cfg-type">string</span>
<span class="cfg cfg-def">Default: <code>1.2</code>, <code>1.3</code></span>

`1.0` to `1.3`. A client may set them only with `"fingerprint": "none"`.

### `handshake_timeout`

<span class="cfg cfg-type">duration</span>
<span class="cfg cfg-def">Default: <code>10s</code></span>

How long the handshake may take.

</div>

A client that fails the TLS handshake is closed, never sent to the inbound's
`fallback`.
