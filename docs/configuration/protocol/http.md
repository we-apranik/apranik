# HTTP

An HTTP proxy: CONNECT tunnels and absolute-form requests (`GET http://…`), with
Basic authentication. TCP only.

## Structure

=== "Inbound"

    ```json
    {
      "tag": "http-in",
      "listen": "127.0.0.1",
      "port": 8080,
      "transport": [{ "type": "tcp" }],
      "protocol": {
        "type": "http",
        "settings": {
          "users": [{ "name": "alice", "password": "correct horse" }],
          "handshake_timeout": "10s"
        }
      }
    }
    ```

=== "Outbound"

    ```json
    {
      "tag": "upstream",
      "server": "203.0.113.5",
      "server_port": 8080,
      "transport": [{ "type": "tcp" }],
      "protocol": {
        "type": "http",
        "settings": { "username": "alice", "password": "correct horse" }
      }
    }
    ```

> [!WARNING]
> Basic authentication sends the password readably. Use it only on a trusted
> network or inside an encrypting transport.

<div class="cfg-fields" markdown>

## Inbound fields

### `users`

<span class="cfg cfg-type">list of users</span>

Each user has a `name`, which may not contain `:`, and a `password`. With none, no login is asked:
anyone who reaches the port may use it, and `check` warns off loopback. See
[Users](../inbound/users.md).

## Outbound fields

### `username`, `password`

<span class="cfg cfg-type">string</span>

The login, if the server asks for one. Set both or neither.

## Fields for both

### `handshake_timeout`

<span class="cfg cfg-type">duration</span>
<span class="cfg cfg-def">Default: <code>10s</code></span>

How long the request (inbound) or the answer to `CONNECT` (outbound) may take.

</div>
