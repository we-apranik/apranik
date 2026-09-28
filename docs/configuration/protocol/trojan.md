# Trojan

Trojan: TCP, and UDP framed inside the same stream. A request with the wrong
password can be handed to the inbound's [`fallback`](../inbound/index.md#fallback),
so a probe sees an ordinary site.

> [!WARNING]
> Trojan has no encryption of its own: run it over [`tls`](../transport/tls.md)
> (optionally with [`ws`](../transport/ws.md) or
> [`httpupgrade`](../transport/httpupgrade.md) above it). Without it, the password
> hash and all payload cross the wire in the clear.

Tested against Xray 26.7 in both directions, over `ws`, `httpupgrade` and
`tls`, for TCP and UDP. Not yet tested against sing-box or trojan-gfw.

## Structure

=== "Inbound"

    ```json
    {
      "tag": "trojan-in",
      "listen": "0.0.0.0",
      "port": 443,
      "transport": [
        { "type": "tcp" },
        { "type": "tls", "settings": { "certificates": [{ "certificate_file": "cert.pem", "key_file": "key.pem" }] } }
      ],
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
      "transport": [{ "type": "tcp" }, { "type": "tls" }],
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

A Trojan outbound also carries UDP: each UDP session opens one stream to the
server, and every destination's datagrams are framed inside it. On an inbound,
Trojan looks at the request's command byte to tell a TCP request from a UDP one
before accepting it.

A request that is not valid Trojan (a wrong password, or not Trojan at all) is
handed to the inbound's [`fallback`](../inbound/index.md#fallback) if one is set,
with every byte it sent. A request shorter than Trojan's header waits for more
bytes until `handshake_timeout`, then is closed.
