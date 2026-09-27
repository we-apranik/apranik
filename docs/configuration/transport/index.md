# Transport

A transport stack is a list of layers under the proxy protocol. It is read
bottom-up: the first entry is a **base transport**, which opens the connection;
each later entry is an **overlay**, which wraps the one before it.

```json
"transport": [
  { "type": "tcp", "settings": { "no_delay": true } }
]
```

Each entry is a module, `{ "type": ..., "settings": {...} }`. `settings` may be
left out for the module's defaults.

| Type | Kind | Format |
| --- | --- | --- |
| `tcp` | base, stream | [TCP](tcp.md) |
| `udp` | base, datagram | [UDP](udp.md) |

> [!NOTE]
> No overlay exists yet. TLS comes next, then WebSocket; today every stack is a
> single `tcp` entry. `udp` appears only inside
> [`freedom`](../outbound/freedom.md#udp)'s settings.

An inbound's stack listens; an outbound's stack reaches the proxy server. The same
overlay twice in one stack is allowed (TLS inside TLS is a real technique), with a
warning.
