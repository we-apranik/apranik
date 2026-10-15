# Run Trojan over WebSocket behind a CDN

A server behind a CDN that relays WebSocket, and a client that reaches it. The
censor sees only a CDN address and TLS to a name on it. Before you start, read
[what these guides assume](index.md).

[![Trojan behind a CDN: the Apranik client sends trojan over ws or httpupgrade over TLS to a CDN address. The censor sees only the CDN address and TLS to the CDN's name. The CDN ends TLS, routes on Host and path, and relays the upgraded connection to the Apranik server, which reads the client's address from X-Forwarded-For and sends traffic out through freedom.](../assets/diagrams/cdn-fronting.excalidraw.svg)](../assets/diagrams/cdn-fronting.excalidraw.svg)

=== "Server"

    ```json
    {
      "version": 1,
      "log": {
        "level": "info",
        "outputs": [{ "type": "stderr" }]
      },
      "inbounds": [
        {
          "tag": "trojan-ws-in",
          "listen": "0.0.0.0",
          "port": 8080,
          "network": "tcp",
          "transport": [
            { "type": "tcp" },
            {
              "type": "ws",
              "settings": { "path": "/chat", "host": "proxy.example.com" }
            }
          ],
          "protocol": {
            "type": "trojan",
            "settings": {
              "users": [{ "name": "alice", "password": "change me" }]
            }
          }
        }
      ],
      "outbounds": [
        {
          "tag": "direct",
          "protocol": { "type": "freedom" }
        }
      ]
    }
    ```

=== "Client"

    ```json
    {
      "version": 1,
      "log": {
        "level": "info",
        "outputs": [{ "type": "stderr" }]
      },
      "inbounds": [
        {
          "tag": "local",
          "listen": "127.0.0.1",
          "port": 1080,
          "network": "tcp",
          "transport": [{ "type": "tcp" }],
          "protocol": {
            "type": "socks5",
            "settings": { "udp_relay": true }
          }
        }
      ],
      "outbounds": [
        {
          "tag": "proxy",
          "server": "proxy.example.com",
          "server_port": 443,
          "transport": [
            { "type": "tcp" },
            {
              "type": "tls",
              "settings": { "alpn": ["http/1.1"] }
            },
            {
              "type": "ws",
              "settings": {
                "path": "/chat",
                "early_data": { "max_bytes": 2048 }
              }
            }
          ],
          "protocol": {
            "type": "trojan",
            "settings": { "password": "change me" }
          }
        }
      ]
    }
    ```

- The CDN ends TLS and reaches the server over plain HTTP on port 8080, so the
  server has no `tls`. If the CDN reaches it over TLS, add `tls` between `tcp` and
  `ws`.
- The client's `"alpn": ["http/1.1"]` is required: otherwise the CDN may choose
  HTTP/2, which `ws` cannot speak.
- `early_data` saves a round trip; the server accepts it without configuration.
- The server reads each client's address from `X-Forwarded-For`; see
  [Client address behind a proxy](../configuration/transport/original_source_ip.md).

> [!WARNING]
> The CDN ends the outer TLS, so it can read the Trojan request, password hash
> included. Use only a CDN you trust with that.

## Trojan over HTTPUpgrade

The same as WebSocket, without framing after the handshake. Swap the `ws`
layer on both sides for:

```json
{
  "type": "httpupgrade",
  "settings": { "path": "/up", "host": "proxy.example.com" }
}
```

Leave `pipeline` off unless both ends are Apranik: Xray's and sing-box's
servers may drop bytes sent before their `101` answer.
