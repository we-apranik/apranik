# SOCKS5

SOCKS5 (RFC 1928) with CONNECT, UDP ASSOCIATE, and username/password
authentication (RFC 1929).

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
          "username": "alice",
          "password": "correct horse",
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

### `username`, `password`

<span class="cfg cfg-type">string</span>, <span class="cfg cfg-type">secret</span>

The one account clients must log in with, up to 255 bytes each. Set both or
neither. With neither, anyone who can reach the inbound may use it, and
`apranik check` warns about it. The accepted name is reported as the user.

### `udp_relay`

<span class="cfg cfg-type">boolean</span>
<span class="cfg cfg-def">Default: <code>true</code></span>

Accept UDP ASSOCIATE. When `false`, the command is refused and clients get TCP
only. The UDP association ends when its control connection closes.

## Outbound

### `username`, `password`

<span class="cfg cfg-type">string</span>, <span class="cfg cfg-type">secret</span>

The account to log in to the server with, if it asks for one.

## Both

### `handshake_timeout`

<span class="cfg cfg-type">duration</span>
<span class="cfg cfg-def">Default: <code>10s</code></span>

How long the SOCKS5 handshake may take: inbound, from the greeting to the
reply, and for UDP ASSOCIATE the reply that follows; outbound, the server's
answers.

</div>

A SOCKS5 outbound carries TCP only.
