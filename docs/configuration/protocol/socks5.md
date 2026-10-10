# SOCKS5

SOCKS5 (RFC 1928) with CONNECT, UDP ASSOCIATE, and username/password
authentication (RFC 1929). An inbound takes a list of [users](../users.md), which can
change while it runs.

## Structure

=== "Inbound"

    ```json
    {
      "tag": "socks-in",
      "listen": "127.0.0.1",
      "port": 1080,
      "transport": [{ "type": "tcp" }],
      "protocol": {
        "type": "socks5",
        "settings": {
          "users": [
            { "name": "alice", "password": "correct horse" },
            { "name": "bob", "password": "battery staple" }
          ],
          "udp_relay": true
        }
      }
    }
    ```

=== "Outbound"

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

<div class="cfg-fields" markdown>

## Inbound

### `users`

<span class="cfg cfg-type">list of { name, password }</span>

Who may log in. A name is the RFC 1929 username; a name and a password are up to
255 bytes each, may be in any language, and may not contain control characters.
A name is unique in the inbound, and the matched name is reported as the user.
Two users may share a password. They can be added and removed
[while the inbound runs](../users.md#changing-users-while-running).

With no users the inbound asks for no login: anyone who can reach it may use it,
and `apranik check` warns about it when it listens beyond this machine.

> [!WARNING]
> Removing the last user removes authentication: the inbound then accepts everyone,
> on any address. This is how "no users" works for SOCKS5 and HTTP. Adding a user
> to an inbound with none makes it ask for a login from then on.

An inbound refuses `username` and `password`, which are an outbound's: the config
would otherwise leave the inbound open.

### `udp_relay`

<span class="cfg cfg-type">boolean</span>
<span class="cfg cfg-def">Default: <code>true</code></span>

Accept UDP ASSOCIATE. When `false`, the command is refused and clients get TCP
only. The UDP association ends when its control connection closes.

### `udp_max_datagram_size`

<span class="cfg cfg-type">integer, bytes</span>
<span class="cfg cfg-def">Default: <code>8192</code></span>

The largest UDP payload an association takes from a client, header aside. A bigger
datagram is dropped and counted (`too_large_from_client`). Each association holds a buffer of
this size plus up to 262 bytes of SOCKS5 header. Match the inbound's
[`udp_max_datagram_size`](../inbound/index.md#udp_max_datagram_size). Must be from `0`
(the default) to 65,507.

## Outbound

### `username`, `password`

<span class="cfg cfg-type">string</span>, <span class="cfg cfg-type">secret</span>

The account to log in to the server with, if it asks for one, up to 255 bytes
each. Set both or neither. An outbound refuses `users`.

## Both

### `handshake_timeout`

<span class="cfg cfg-type">duration</span>
<span class="cfg cfg-def">Default: <code>10s</code></span>

How long the SOCKS5 handshake may take: inbound, from the greeting to the
reply, and for UDP ASSOCIATE the reply that follows; outbound, the server's
answers.

</div>

A SOCKS5 outbound carries TCP only.
