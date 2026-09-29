# TLS

An overlay stream transport: TLS 1.2 and 1.3 over the layer below it. On an
outbound it is the client and checks the server's certificate; on an inbound it
is the server and presents one.

> [!NOTE]
> **A client imitates Chrome by default.** Go's own ClientHello is shared by
> every Go program, so a censor can single it out without decrypting anything.
> The client sends a browser's instead, through
> [uTLS](https://github.com/refraction-networking/utls): Chrome unless
> [`fingerprint`](#fingerprint) says otherwise. Set `"fingerprint": "none"`
> for Go's own. REALITY and ECH are not supported.

## Structure

=== "Inbound"

    ```json
    {
      "transport": [
        { "type": "tcp" },
        {
          "type": "tls",
          "settings": {
            "certificates": [
              { "certificate_file": "/etc/apranik/cert.pem", "key_file": "/etc/apranik/key.pem" }
            ],
            "alpn": ["http/1.1"]
          }
        }
      ]
    }
    ```

=== "Outbound"

    ```json
    {
      "server": "203.0.113.5",
      "server_port": 443,
      "transport": [
        { "type": "tcp" },
        {
          "type": "tls",
          "settings": {
            "server_name": "example.com",
            "fingerprint": "chrome",
            "alpn": ["http/1.1"]
          }
        }
      ]
    }
    ```

<div class="cfg-fields" markdown>

## Outbound

### `server_name`

<span class="cfg cfg-type">string</span>
<span class="cfg cfg-def">Default: the server's domain</span>

The name sent as SNI and checked against the server's certificate. When the
outbound's `server` is an IP and this is empty, no SNI is sent and the
certificate must list that IP.

### `insecure`

<span class="cfg cfg-type">boolean</span>
<span class="cfg cfg-def">Default: <code>false</code></span>

Accept any certificate. Anyone on the path can then read and change the
traffic. Warned about.

### `fingerprint`

<span class="cfg cfg-type">string</span>
<span class="cfg cfg-def">Default: <code>chrome</code></span>

Whose ClientHello to send. The names are the ones Xray and sing-box use, so an
`fp=` value from a share link carries over:

| Name | Imitates |
| --- | --- |
| `chrome` | Chrome (uTLS's newest, 133) |
| `firefox` | Firefox (uTLS's newest, 120) |
| `safari` | Safari (uTLS's newest, 16.0) |
| `ios`, `android`, `edge`, `360`, `qq` | Safari on iOS 14, OkHttp on Android 11, Edge 85, 360 Browser 7.5, QQ Browser 11.1: old, warned about |
| `random` | one of `chrome`, `firefox`, `edge`, `safari`, `ios`, chosen when Apranik starts |
| `randomized` | a made-up ClientHello, fixed when Apranik starts |
| `none` | Go's own; the only value that allows `min_version` and `max_version` |

A specific version is named as uTLS names it, lowercased, as in Xray:
`hellochrome_133`, `hellochrome_131`, `hellochrome_120`, `hellofirefox_120`,
`hellosafari_16_0`, `helloios_13`, and the others uTLS exports except its beta
variants (`_psk`, `_pq`, `_shuffle`) and the two it calls incompatible
(`helloedge_106`, `hello360_11_0`). A version older than its browser's newest is
warned about: a browser few people still run may stand out rather than blend
in.

The preset fixes everything in the ClientHello except the ALPN list, which
[`alpn`](#alpn) replaces when set. Setting `min_version` or `max_version` with a
fingerprint is refused, since the preset fixes the versions. A TLS 1.3 session is not resumed with a fingerprint, as
in Xray and sing-box, so each connection makes a full handshake. A server
ignores `fingerprint`.

### `certificate_authorities`

<span class="cfg cfg-type">list of { certificate_file | certificate }</span>

Certificate authorities trusted in addition to the system's. Each entry is a PEM
file path or inline PEM, not both.

## Inbound

### `certificates`

<span class="cfg cfg-type">list of { certificate_file | certificate, key_file | key }</span>
<span class="cfg cfg-req">Required</span>

The certificate chains to present, each with its private key, as file paths or
inline PEM. With several, the one matching the client's SNI is used. They are
read when `apranik run` starts, so a missing file or a mismatched key stops
startup; `apranik check` does not read them. An inline `key` never appears in
logs or errors.

## Both

### `alpn`

<span class="cfg cfg-type">list of strings</span>

Application protocols, in preference order: offered by the client, accepted by
the server. A server with a list refuses a client whose offer shares nothing
with it. On a client with a fingerprint, it replaces the browser's list and
nothing else; left unset, the browser's list is sent (`h2`, `http/1.1` for
Chrome).

Under [`ws`](ws.md) or [`httpupgrade`](httpupgrade.md), which speak only
HTTP/1.1, set it to `["http/1.1"]`. If TLS offers `h2` there, a server or CDN
may choose it and the connection fails after the handshake, so `apranik check`
refuses a list with anything else, on inbounds and outbounds:

```
outbounds[0].transport[1].settings.alpn: tls offers [h2 http/1.1], but ws above it can speak only [http/1.1], and the other end may choose h2; set alpn to ["http/1.1"]
```

One settings type serves both roles, so `apranik check` takes `tls` settings
without `certificates` to be a client's (a server cannot run without them) and
applies the default fingerprint to them. Chrome itself offers only `http/1.1`
when it opens a connection for a WebSocket.

### `min_version`, `max_version`

<span class="cfg cfg-type">string</span>
<span class="cfg cfg-def">Default: Go's, <code>1.2</code> and <code>1.3</code></span>

One of `1.0`, `1.1`, `1.2`, `1.3`. A minimum below `1.2` is warned about. A
client may set them only with `"fingerprint": "none"`.

</div>

The handshake runs per connection: on an inbound within `handshake_timeout`,
on an outbound within the dial timeout. A client that fails the handshake is
closed, not handed to the inbound's `fallback`; a fallback receives only what a
client sent inside TLS.
