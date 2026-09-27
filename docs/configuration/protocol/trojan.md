# Trojan

Trojan: TCP, and UDP framed inside the same stream. A request with the wrong
password can be handed to the inbound's [`fallback`](../inbound/index.md#fallback),
so a probe sees an ordinary site.

> [!WARNING]
> Trojan has no encryption of its own, and no TLS overlay exists in Apranik yet.
> Today a Trojan stack sends its password hash and all payload in the clear.
> Interoperability with other Trojan implementations has not been tested.

## Structure

=== "Inbound"

    ```json
    {
      "tag": "trojan-in",
      "listen": "0.0.0.0",
      "port": 443,
      "transport": [{ "type": "tcp" }],
      "protocol": {
        "type": "trojan",
        "settings": {
          "users": [
            { "name": "alice", "password": "correct horse" },
            { "name": "bob", "password": "battery staple" }
          ]
        }
      },
      "fallback": { "server": "127.0.0.1", "server_port": 8080 }
    }
    ```

=== "Outbound"

    ```json
    {
      "tag": "trojan-out",
      "server": "203.0.113.5",
      "server_port": 443,
      "transport": [{ "type": "tcp" }],
      "protocol": {
        "type": "trojan",
        "settings": { "password": "correct horse" }
      }
    }
    ```

<div class="cfg-fields" markdown>

## Inbound

### `users`

<span class="cfg cfg-type">list of { name, password }</span>
<span class="cfg cfg-req">Required</span>
<span class="cfg cfg-note">provisional</span>

Who may connect. Names and passwords must each be unique, so a password always
identifies one user; the matched name is reported as the user. This list may move
once user management is designed.

## Outbound

### `password`

<span class="cfg cfg-type">secret</span>
<span class="cfg cfg-req">Required</span>

The password sent to the server.

</div>

The client sends its request together with the first payload, waiting up to
100ms for it before sending the request alone.
