# SOCKS5

SOCKS5 (RFC 1928): CONNECT, UDP ASSOCIATE, and username and password login
(RFC 1929). An outbound carries TCP only.

## Structure

:::tabs
== Inbound

```json
{
  "tag": "socks-in",
  "listen": "127.0.0.1",
  "port": 1080,
  "transport": [{ "type": "tcp" }],
  "protocol": {
    "type": "socks5",
    "settings": {
      "users": [{ "name": "alice", "password": "correct horse" }],
      "udp_relay": true,
      "udp_max_datagram_size": 8192,
      "handshake_timeout": "10s"
    }
  }
}
```

== Outbound

```json
{
  "tag": "upstream",
  "server": "203.0.113.5",
  "server_port": 1080,
  "transport": [{ "type": "tcp" }],
  "protocol": {
    "type": "socks5",
    "settings": { "username": "alice", "password": "correct horse" }
  }
}
```

:::

<div class="cfg-fields">

## Inbound fields

### `users`

<span class="cfg cfg-type">list of users</span>

Each user has a `name` and a `password`, up to 255 bytes each. With none, no login is asked:
anyone who reaches the port may use it, and `check` warns off loopback. See
[Users](../inbound/users.md).

### `udp_relay`

<span class="cfg cfg-type">boolean</span>
<span class="cfg cfg-def">Default: <code>true</code></span>

Accept UDP ASSOCIATE. The association ends when its control connection closes.

### `udp_max_datagram_size`

<span class="cfg cfg-type">integer, bytes</span>
<span class="cfg cfg-def">Default: <code>8192</code></span>

The largest UDP payload taken from a client, at most 65,507. Match the inbound's
[`udp_max_datagram_size`](../inbound/index.md#udp_max_datagram_size).

## Outbound fields

### `username`, `password`

<span class="cfg cfg-type">string</span>

The login, if the server asks for one. Set both or neither.

## Fields for both

### `handshake_timeout`

<span class="cfg cfg-type">duration</span>
<span class="cfg cfg-def">Default: <code>10s</code></span>

How long the SOCKS5 handshake may take.

</div>
