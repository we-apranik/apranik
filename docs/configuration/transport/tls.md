# TLS

An overlay stream transport: TLS 1.2 and 1.3 over the layer below it. On an
outbound it is the client and checks the server's certificate; on an inbound it
is the server and presents one.

> [!WARNING]
> The client's ClientHello is Go's own, not a browser's, so a censor can tell
> it apart. Browser fingerprints (uTLS), REALITY and ECH are not supported.

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
with it.

### `min_version`, `max_version`

<span class="cfg cfg-type">string</span>
<span class="cfg cfg-def">Default: <code>1.2</code>, <code>1.3</code></span>

One of `1.0`, `1.1`, `1.2`, `1.3`. A minimum below `1.2` is warned about.

</div>

The handshake runs per connection: on an inbound within `handshake_timeout`,
on an outbound within the dial timeout. A client that fails the handshake is
closed, not handed to the inbound's `fallback`; a fallback receives only what a
client sent inside TLS.
