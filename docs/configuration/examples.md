# Examples

Complete config files for common setups. Each pair is a **server**, run on a
machine outside the censored network, and a **client**, run on the user's
device, which exposes a local SOCKS5 or mixed port for apps.

Replace `proxy.example.com`, the passwords and the certificate paths with your
own. Check a file before running it:

```sh
apranik check -config server.json
apranik run -config server.json
```

On a client, `check` warns that the local SOCKS5 or HTTP port has no username.
That is expected for a port bound to `127.0.0.1`; set one if other users or
apps on the machine should not use it.

The protocol and transport pages describe every setting:
[`trojan`](protocol/trojan.md), [`tls`](transport/tls.md),
[`ws`](transport/ws.md), [`httpupgrade`](transport/httpupgrade.md).

## Trojan over TLS

The simplest censorship-resistant setup: the server looks like an HTTPS site.
A request with the wrong password is handed to `fallback`, so a prober sees
whatever runs there (an ordinary web server on the same machine, for example).

=== "Server"

    ```json
    {
      "version": 1,
      "log": { "level": "info", "outputs": [{ "type": "stderr" }] },
      "inbounds": [
        {
          "tag": "trojan-in",
          "listen": "0.0.0.0",
          "port": 443,
          "network": "tcp",
          "transport": [
            { "type": "tcp" },
            {
              "type": "tls",
              "settings": {
                "certificates": [
                  {
                    "certificate_file": "/etc/apranik/fullchain.pem",
                    "key_file": "/etc/apranik/privkey.pem"
                  }
                ],
                "alpn": ["http/1.1"]
              }
            }
          ],
          "protocol": {
            "type": "trojan",
            "settings": { "users": [{ "name": "alice", "password": "change me" }] }
          },
          "fallback": { "server": "127.0.0.1", "server_port": 8080 }
        }
      ],
      "outbounds": [{ "tag": "direct", "protocol": { "type": "freedom" } }]
    }
    ```

=== "Client"

    ```json
    {
      "version": 1,
      "log": { "level": "info", "outputs": [{ "type": "stderr" }] },
      "inbounds": [
        {
          "tag": "local",
          "listen": "127.0.0.1",
          "port": 1080,
          "network": "tcp",
          "transport": [{ "type": "tcp" }],
          "protocol": {
            "type": "mixed",
            "settings": { "socks5": { "udp_relay": true }, "http": {} }
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
            { "type": "tls", "settings": { "alpn": ["http/1.1"] } }
          ],
          "protocol": { "type": "trojan", "settings": { "password": "change me" } }
        }
      ]
    }
    ```

The client's `tls` sends `proxy.example.com` as the SNI and checks the server's
certificate against the system's roots. With a self-signed certificate, add it
under `certificate_authorities` rather than turning on `insecure`. It sends
Chrome's ClientHello by default ([`fingerprint`](transport/tls.md#fingerprint)),
with its ALPN narrowed to `http/1.1`.

## Trojan over WebSocket behind a CDN

The client connects to the CDN, not to the server: the censor sees a CDN
address and TLS to a name on it. The CDN relays the WebSocket upgrade to the
server.

[![Trojan over WebSocket or HTTPUpgrade behind a CDN: the Apranik client speaks trojan over ws or httpupgrade over tls to a CDN address; the censor sees a CDN IP and the SNI only; the CDN ends TLS, routes on Host and path, and relays the upgraded connection to the Apranik server, which sends traffic out through freedom.](../assets/diagrams/cdn-fronting.excalidraw.svg)](../assets/diagrams/cdn-fronting.excalidraw.svg)

=== "Server"

    ```json
    {
      "version": 1,
      "log": { "level": "info", "outputs": [{ "type": "stderr" }] },
      "inbounds": [
        {
          "tag": "trojan-ws-in",
          "listen": "0.0.0.0",
          "port": 8080,
          "network": "tcp",
          "transport": [
            { "type": "tcp" },
            { "type": "ws", "settings": { "path": "/chat", "host": "proxy.example.com" } }
          ],
          "protocol": {
            "type": "trojan",
            "settings": { "users": [{ "name": "alice", "password": "change me" }] }
          }
        }
      ],
      "outbounds": [{ "tag": "direct", "protocol": { "type": "freedom" } }]
    }
    ```

=== "Client"

    ```json
    {
      "version": 1,
      "log": { "level": "info", "outputs": [{ "type": "stderr" }] },
      "inbounds": [
        {
          "tag": "local",
          "listen": "127.0.0.1",
          "port": 1080,
          "network": "tcp",
          "transport": [{ "type": "tcp" }],
          "protocol": { "type": "socks5", "settings": { "udp_relay": true } }
        }
      ],
      "outbounds": [
        {
          "tag": "proxy",
          "server": "proxy.example.com",
          "server_port": 443,
          "transport": [
            { "type": "tcp" },
            { "type": "tls", "settings": { "alpn": ["http/1.1"] } },
            {
              "type": "ws",
              "settings": { "path": "/chat", "max_early_data": 2048 }
            }
          ],
          "protocol": { "type": "trojan", "settings": { "password": "change me" } }
        }
      ]
    }
    ```

Here the CDN terminates TLS and connects to the server over plain HTTP on
port 8080, so the server's stack has no `tls`. If the CDN connects to the origin
over TLS, add a `tls` layer with a certificate between `tcp` and `ws` on the
server. `max_early_data` saves a round trip per connection; the server accepts
it without configuration. The client's `"alpn": ["http/1.1"]` is required: a
CDN that supports HTTP/2 would otherwise choose it, and `ws` speaks HTTP/1.1
([why](transport/tls.md#alpn)).

> [!WARNING]
> The CDN ends the outer TLS, so it can read the Trojan request, including the
> password hash. Only use a CDN you trust with that. Behind a CDN the server
> sees the CDN's address as every client's source (open question Q19).

## Trojan over HTTPUpgrade

The same as WebSocket, without framing after the handshake. Swap the `ws`
layer on both sides for:

```json
{ "type": "httpupgrade", "settings": { "path": "/up", "host": "proxy.example.com" } }
```

Leave `pipeline` off unless both ends are Apranik: Xray's and sing-box's
servers can drop bytes sent before their `101` answer.

## A local proxy with no server

Useful to try Apranik or to give apps one local port that sends everything
straight out. Sniffing fills in the domain for connections that name an IP.

```json
{
  "version": 1,
  "log": { "level": "info", "outputs": [{ "type": "stderr", "format": "human" }] },
  "inbounds": [
    {
      "tag": "local",
      "listen": "127.0.0.1",
      "port": 1080,
      "network": "tcp",
      "transport": [{ "type": "tcp" }],
      "protocol": {
        "type": "mixed",
        "settings": { "socks5": { "udp_relay": true }, "http": {} }
      },
      "sniffing": { "sniffers": [{ "type": "tls" }, { "type": "http" }] }
    }
  ],
  "outbounds": [{ "tag": "direct", "protocol": { "type": "freedom" } }]
}
```

## Mixing and matching

Any proxy protocol runs over any stream stack, and the same overlay can appear
on an inbound and an outbound. Some combinations that work today:

| Stack | Use |
| --- | --- |
| `tcp` | local proxies; Trojan in the clear is only for testing |
| `tcp → tls` | a server that looks like an HTTPS site |
| `tcp → tls → ws` | behind a CDN that relays WebSocket, or directly |
| `tcp → tls → httpupgrade` | the same, cheaper, where frames are not checked |
| `tcp → ws` / `tcp → httpupgrade` | an origin behind a CDN that terminates TLS |
