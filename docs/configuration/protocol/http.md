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
        "settings": { "username": "alice", "password": "correct horse" }
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
      "protocol": { "type": "http" }
    }
    ```

<div class="cfg-fields" markdown>

## Inbound

### `username`, `password`

<span class="cfg cfg-type">string</span>, <span class="cfg cfg-type">secret</span>

The one account clients must log in with. Set both or neither. With neither,
anyone who can reach the inbound may use it, and `apranik check` warns about it.

## Outbound

### `username`, `password`

<span class="cfg cfg-type">string</span>, <span class="cfg cfg-type">secret</span>

The account to log in to the server with, if it asks for one.

</div>

> [!WARNING]
> Basic authentication sends the password readably. Use it only on a trusted
> network or over an encrypting transport.
