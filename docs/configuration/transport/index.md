# Transport

A transport stack is the list of layers under a proxy protocol, in an inbound's or
outbound's `transport`. The first entry is a **base** transport, which opens the
connection; each later entry is an **overlay**, which wraps the one before it.

[![How a transport stack is built: an outbound's list, such as tcp, tls, ws, is dialed in order: tcp opens the connection, tls wraps it, ws wraps that, and the proxy protocol sends its request over the result. An inbound with the same list accepts in the same order and its proxy protocol reads the request.](../../assets/diagrams/transport-stack.excalidraw.svg)](../../assets/diagrams/transport-stack.excalidraw.svg)

## Structure

<div class="cfg-structure">

```json
{
  "transport": [
    {
      "type": "tcp",
      "settings": { "no_delay": true }
    },
    {
      "type": "tls",
      "settings": { "server_name": "proxy.example.com" }
    }
  ]
}
```

</div>

Each entry has a `type` and optional `settings`.

| Type | Kind | Where |
| --- | --- | --- |
| [`tcp`](tcp.md) | base | inbound, outbound |
| [`udp`](udp.md) | base, datagram | freedom's `settings.udp` |
| [`tls`](tls.md) | overlay | inbound, outbound |
| [`ws`](ws.md) | overlay | inbound, outbound |
| [`httpupgrade`](httpupgrade.md) | overlay | inbound, outbound |
| [`fragment`](fragment.md) | overlay | outbound |
| [`proxy_protocol`](proxy_protocol.md) | overlay | inbound |

## Choosing a stack

| Goal | Stack |
| --- | --- |
| Look like an HTTPS site | `tcp`, `tls` |
| Pass a CDN | `tcp`, `tls`, `ws`; or `tcp`, `ws` where the CDN ends TLS |
| The same, cheaper after the handshake | `httpupgrade` instead of `ws`, where nothing checks frames |
| Hide the SNI from DPI that does not reassemble | `fragment` directly below `tls` |
| Learn the client's address behind a load balancer | `tcp`, `proxy_protocol` |
