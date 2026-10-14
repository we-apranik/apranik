# Guides

Task-based walkthroughs with complete config files. Each pair is a **server**, run on a
machine outside the censored network, and a **client**, run on the user's device, which
exposes a local SOCKS5 or mixed port for apps.

[![A client and a server: an app on the user's device talks SOCKS5 or HTTP to the local apranik client, whose outbound is a Trojan proxy over TLS. Across the censored network a DPI box sees only a TLS connection to the server's name. On the server, an apranik inbound with the Trojan protocol over TLS passes the request to freedom, which connects to the real destination.](../assets/diagrams/client-and-server.excalidraw.svg)](../assets/diagrams/client-and-server.excalidraw.svg)

## What these guides assume

Replace `proxy.example.com`, the passwords and the certificate paths with your
own. Check a file before running it:

```sh
apranik check -config server.json
apranik run -config server.json
```

A client's local port with no `users` is open to every app on the machine.

The protocol and transport pages describe every setting:
[`trojan`](../configuration/protocol/trojan.md), [`tls`](../configuration/transport/tls.md),
[`ws`](../configuration/transport/ws.md), [`httpupgrade`](../configuration/transport/httpupgrade.md).

## The guides

- [Run Trojan over TLS](trojan-over-tls.md): A server that looks like an HTTPS site, and a client that reaches it.
- [Run Trojan over WebSocket behind a CDN](trojan-over-websocket-cdn.md): A server behind a CDN that relays WebSocket, and a client that reaches it.
- [Run a local proxy with no server](local-proxy.md): A proxy on your own machine that needs no server.
- [Hide the server name](hide-the-server-name.md): Keep the server's name out of the TLS handshake.
