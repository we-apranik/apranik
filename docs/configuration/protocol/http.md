# HTTP

An HTTP proxy: CONNECT tunnels and absolute-form requests (`GET http://…`), with
Basic authentication. TCP only. An inbound takes a list of [users](../users.md), which
can change while it runs.

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
          "users": [
            { "name": "alice", "password": "correct horse" },
            { "name": "bob", "password": "battery staple" }
          ]
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
      "protocol": { "type": "http" }
    }
    ```

<div class="cfg-fields" markdown>

## Inbound

### `users`

<span class="cfg cfg-type">list of { name, password }</span>

Who may log in with Basic authentication. A name is unique in the inbound, may be in
any language, and may not contain `:` (Basic authentication uses it to separate the
name from the password) or a control character; neither may a password contain a
control character. A password may contain `:`. The matched name is reported as the
user. Users can be added and removed [while the inbound runs](../users.md#changing-users-while-running).

With no users the inbound asks for no login: anyone who can reach it may use it,
and `apranik check` warns about it when it listens beyond this machine.

> [!WARNING]
> Removing the last user removes authentication: the inbound then accepts everyone,
> on any address. This is how "no users" works for SOCKS5 and HTTP. Adding a user
> to an inbound with none makes it ask for a login from then on.

An inbound refuses `username` and `password`, which are an outbound's: the config
would otherwise leave the inbound open.

## Outbound

### `username`, `password`

<span class="cfg cfg-type">string</span>, <span class="cfg cfg-type">secret</span>

The account to log in to the server with, if it asks for one. Set both or neither.
An outbound refuses `users`.

## Both

### `handshake_timeout`

<span class="cfg cfg-type">duration</span>
<span class="cfg cfg-def">Default: <code>10s</code></span>

How long the request may take to arrive (inbound), or the proxy's answer to
`CONNECT` (outbound).

</div>

> [!WARNING]
> Basic authentication sends the password readably. Use it only on a trusted
> network or over an encrypting transport.
