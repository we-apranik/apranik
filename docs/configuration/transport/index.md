# Transport

A transport stack is a list of layers under the proxy protocol. It is read
bottom-up: the first entry is a **base transport**, which opens the connection;
each later entry is an **overlay**, which wraps the one before it.

```json
"transport": [
  { "type": "tcp", "settings": { "no_delay": true } },
  { "type": "tls", "settings": { "server_name": "example.com" } }
]
```

Each entry is a module, `{ "type": ..., "settings": {...} }`. `settings` may be
left out for the module's defaults.

| Type | Kind | Format |
| --- | --- | --- |
| `tcp` | base, stream | [TCP](tcp.md) |
| `udp` | base, datagram | [UDP](udp.md) |
| `tls` | overlay, stream | [TLS](tls.md) |
| `ws` | overlay, stream | [WebSocket](ws.md) |
| `httpupgrade` | overlay, stream | [HTTPUpgrade](httpupgrade.md) |

> [!NOTE]
> `udp` appears only inside [`freedom`](../outbound/freedom.md#udp)'s settings.
> Overlays run once per connection, after the base has connected or accepted,
> and within the inbound's `handshake_timeout` or the outbound's dial timeout.

An inbound's stack listens; an outbound's stack reaches the proxy server. The same
overlay twice in one stack is allowed (TLS inside TLS is a real technique), with a
warning.

## Choosing a stack

| Goal | Stack |
| --- | --- |
| Look like an HTTPS site | `tcp → tls` |
| Pass a CDN or HTTP reverse proxy | `tcp → tls → ws`, or `tcp → ws` when the CDN ends TLS |
| The same, with no cost after the handshake | `httpupgrade` in place of `ws`, where nothing checks WebSocket frames |

[![What ws and httpupgrade put on the wire: both send the same HTTP/1.1 upgrade request and get a 101 answer; ws then carries the payload in WebSocket frames with a header and, from the client, a mask; httpupgrade carries the payload as raw bytes.](../../assets/diagrams/ws-vs-httpupgrade.excalidraw.svg)](../../assets/diagrams/ws-vs-httpupgrade.excalidraw.svg)

[Examples](../examples.md) has complete server and client files for each.
