# Mixed

`mixed` serves SOCKS5 and HTTP on one port. It is a special inbound protocol,
built by core rather than a module.

## Structure

<div class="cfg-structure" markdown>

```json
{
  "tag": "mixed-in",
  "listen": "127.0.0.1",
  "port": 2080,
  "transport": [{ "type": "tcp" }],
  "protocol": {
    "type": "mixed",
    "settings": {
      "socks5": { "udp_relay": true },
      "http": {}
    }
  }
}
```

</div>

The other fields are those of every [inbound](index.md).

<div class="cfg-fields" markdown>

## Fields

### `socks5`

<span class="cfg cfg-type">object</span>
<span class="cfg cfg-def">Default: the module's defaults</span>

[SOCKS5 settings](../protocol/socks5.md#inbound) for the SOCKS5 half.

### `http`

<span class="cfg cfg-type">object</span>
<span class="cfg cfg-def">Default: the module's defaults</span>

[HTTP settings](../protocol/http.md#inbound) for the HTTP half.

</div>

## How it chooses

Core reads the first byte of each connection: `0x05` goes to SOCKS5, anything
else to HTTP. SOCKS5 UDP ASSOCIATE works as on a `socks5` inbound.

[![How mixed dispatches a connection: the accept loop hands the stream to mixed, which peeks exactly one byte and passes the stream to the socks5 module on 0x05 or to the http module otherwise; for UDP ASSOCIATE core calls Associate on its own carrier, forwarded by mixed to the socks5 module.](../../assets/diagrams/mixed-dispatch.excalidraw.svg)](../../assets/diagrams/mixed-dispatch.excalidraw.svg)

Each half keeps its own credentials, and a half without a username is warned
about at its own path (`protocol.settings.socks5` or `protocol.settings.http`).
`mixed` is refused on a `udp` inbound and on an outbound. SOCKS4 is not supported:
its first byte goes to HTTP, which refuses it. See
a design note.
