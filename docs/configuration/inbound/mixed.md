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
      "users": [{ "name": "alice", "password": "correct horse" }],
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

### `users`

<span class="cfg cfg-type">list of { name, password }</span>

Who may log in, one list for both protocols: the same name and password work over
SOCKS5 and over HTTP, and the matched name is reported as the user. A user is
accepted only if both protocols accept it, so a name with a `:` (which HTTP Basic
cannot carry) or a name or password over 255 bytes (which SOCKS5 cannot) is refused,
and `apranik check` says which protocol refused which entry. With no users, both halves
ask for no login. They can be added and removed
[while the inbound runs](../users.md#changing-users-while-running).

Users are written here and not under `socks5` or `http`, which refuse them.

### `socks5`

<span class="cfg cfg-type">object</span>
<span class="cfg cfg-def">Default: the module's defaults</span>

[SOCKS5 settings](../protocol/socks5.md#inbound) for the SOCKS5 half.

### `http`

<span class="cfg cfg-type">object</span>
<span class="cfg cfg-def">Default: the module's defaults</span>

[HTTP settings](../protocol/http.md#inbound) for the HTTP half.

### `first_byte_timeout`

<span class="cfg cfg-type">duration</span>
<span class="cfg cfg-def">Default: <code>10s</code></span>

How long a client may take to send the first byte, which picks SOCKS5 or HTTP.
After that, the chosen protocol's own `handshake_timeout` applies, from its
settings above.

</div>

## How it chooses

Core reads the first byte of each connection: `0x05` goes to SOCKS5, anything
else to HTTP. SOCKS5 UDP ASSOCIATE works as on a `socks5` inbound.

[![How mixed dispatches a connection: the accept loop hands the stream to mixed, which peeks exactly one byte and passes the stream to the socks5 module on 0x05 or to the http module otherwise; for UDP ASSOCIATE core calls Associate on its own carrier, forwarded by mixed to the socks5 module.](../../assets/diagrams/mixed-dispatch.excalidraw.svg)](../../assets/diagrams/mixed-dispatch.excalidraw.svg)

With no `users`, each half is warned about at its own path
(`protocol.settings.socks5` or `protocol.settings.http`) when the inbound listens
beyond this machine. `mixed` is refused on a `udp` inbound and on an outbound. SOCKS4 is not supported:
its first byte goes to HTTP, which refuses it. See
a design note.
