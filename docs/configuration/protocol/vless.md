# VLESS

A UUID, a command and a destination, then raw payload: TCP, and UDP framed inside
the stream, plain or as XUDP. A request from an unknown user can go to the
inbound's [`fallback`](../inbound/index.md#fallback).

> [!WARNING]
> VLESS does not encrypt. Run it over [`tls`](../transport/tls.md), or the UUID and
> all traffic cross the wire in the clear.

Not built yet: `flow` (XTLS Vision), TCP over Mux, plain Mux.Cool UDP, reverse proxy
and VLESS encryption. A known user asking for one is refused.

## Structure

:::tabs
== Inbound

```json
{
  "tag": "vless-in",
  "listen": "0.0.0.0",
  "port": 443,
  "transport": [
    { "type": "tcp" },
    {
      "type": "tls",
      "settings": {
        "certificates": [
          {
            "certificate_file": "/etc/apranik/cert.pem",
            "key_file": "/etc/apranik/key.pem"
          }
        ]
      }
    }
  ],
  "protocol": {
    "type": "vless",
    "settings": {
      "users": [
        { "name": "alice", "uuid": "b831381d-6324-4d53-ad4f-8cda48b30811" }
      ],
      "handshake_timeout": "10s"
    }
  },
  "fallback": { "server": "127.0.0.1", "server_port": 8080 }
}
```

== Outbound

```json
{
  "tag": "vless-out",
  "server": "proxy.example.com",
  "server_port": 443,
  "transport": [{ "type": "tcp" }, { "type": "tls" }],
  "protocol": {
    "type": "vless",
    "settings": { "uuid": "b831381d-6324-4d53-ad4f-8cda48b30811" }
  }
}
```

:::

<div class="cfg-fields">

## Inbound fields

### `users`

<span class="cfg cfg-type">list of users</span>
<span class="cfg cfg-req">Required</span>

At least one, each with a `name` and a unique `uuid`. The name is Apranik's own,
for routing, statistics and logs. See [Users](../inbound/users.md).

### `handshake_timeout`

<span class="cfg cfg-type">duration</span>
<span class="cfg cfg-def">Default: <code>10s</code></span>

How long a client may take to send its request.

## Outbound fields

### `uuid`

<span class="cfg cfg-type">string</span>
<span class="cfg cfg-req">Required</span>

The user's UUID.

</div>

A `uuid` is read as Xray and sing-box read it: a UUID, with or without dashes, or 1
to 30 bytes of text turned into a version 5 UUID.

## UDP

- **XUDP**, the default: one stream carries a UDP session's datagrams to every
  destination. Set by [`muxcool`](../shared/muxcool.md).
- **Plain UDP**, with `"muxcool": { "udp": { "enabled": false } }`: one stream per
  destination, for servers without Mux.Cool.

> [!NOTE]
> An Xray client with `mux.enabled` and no `xudpConcurrency` puts TCP and UDP in one
> Mux connection, which an Apranik inbound refuses. Set `"concurrency": -1` and a
> positive `xudpConcurrency` on the client.
