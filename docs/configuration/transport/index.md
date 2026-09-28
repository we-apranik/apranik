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

> [!NOTE]
> `udp` appears only inside [`freedom`](../outbound/freedom.md#udp)'s settings.
> Overlays run once per connection, after the base has connected or accepted,
> and within the inbound's `handshake_timeout` or the outbound's dial timeout.

An inbound's stack listens; an outbound's stack reaches the proxy server. The same
overlay twice in one stack is allowed (TLS inside TLS is a real technique), with a
warning.
