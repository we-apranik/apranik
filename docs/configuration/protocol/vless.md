# VLESS

VLESS: a UUID, a command and a destination in a header, then raw payload. TCP, and
plain UDP framed inside the stream. A request from an unknown user can be handed to the
inbound's [`fallback`](../inbound/index.md#fallback), so a probe sees an ordinary site. An
inbound takes a list of [users](../users.md), which can change while it runs.

> [!WARNING]
> VLESS has no encryption of its own: run it over [`tls`](../transport/tls.md)
> (optionally with [`ws`](../transport/ws.md) or
> [`httpupgrade`](../transport/httpupgrade.md) above it). Without it, the UUID and all
> payload cross the wire in the clear.

Tested against Xray 26.10 and sing-box in both directions, over plain `tcp`, for TCP and plain
UDP. See [interoperability](../../contributors/interop.md).

> [!NOTE]
> Not built yet: `flow` (XTLS Vision), Mux and its UDP form, XUDP, reverse proxy and VLESS
> "encryption". A request from a known user that asks for one is refused with an error, and
> never handed to the fallback. Xray's and sing-box's clients send most UDP as XUDP by
> default (Xray: all but ports 53 and 443; sing-box: all of it), so their UDP does not work
> through an Apranik inbound until XUDP is built. Their TCP does. Setting sing-box's
> `packet_encoding` to `""`, or Xray's environment variable `XRAY_CONE_DISABLED=true`, makes
> them send plain UDP.

## Structure

=== "Inbound"

    ```json
    {
      "tag": "vless-in",
      "listen": "0.0.0.0",
      "port": 443,
      "transport": [
        { "type": "tcp" },
        { "type": "tls", "settings": { "certificates": [{ "certificate_file": "cert.pem", "key_file": "key.pem" }] } }
      ],
      "protocol": {
        "type": "vless",
        "settings": {
          "users": [
            { "name": "alice", "uuid": "b831381d-6324-4d53-ad4f-8cda48b30811" },
            { "name": "bob", "uuid": "a3482e88-686a-4a58-8126-99c9df64b7bf" }
          ]
        }
      },
      "fallback": { "server": "127.0.0.1", "server_port": 8080 }
    }
    ```

=== "Outbound"

    ```json
    {
      "tag": "vless-out",
      "server": "203.0.113.5",
      "server_port": 443,
      "transport": [{ "type": "tcp" }, { "type": "tls" }],
      "protocol": {
        "type": "vless",
        "settings": { "uuid": "b831381d-6324-4d53-ad4f-8cda48b30811" }
      }
    }
    ```

<div class="cfg-fields" markdown>

## Inbound

### `users`

<span class="cfg cfg-type">list of { name, uuid }</span>
<span class="cfg cfg-req">Required</span>

Who may connect, with at least one user at start. `name` is Apranik's own: it is what
[routing](../routing/index.md), statistics and logs call the user. `uuid` is what VLESS
puts on the wire. Names and UUIDs must each be unique, so a UUID always identifies one
user. Names may be in any language and may not contain control characters. Users can be
added and removed [while the inbound runs](../users.md#changing-users-while-running), and
the same people connect over TCP and UDP. An inbound with no users left refuses every
client until one is added.

A `uuid` is written the way Xray and sing-box accept it: a UUID, with or without dashes, or
any text of 1 to 30 bytes, which becomes a UUID version 5 in the nil namespace. A client and a
server given the same text agree on it. Text of 31 bytes, or a UUID-looking text that is not
one, is refused.

An inbound refuses `uuid` and `max_first_payload_delay`, which are an outbound's.

### `handshake_timeout`

<span class="cfg cfg-type">duration</span>
<span class="cfg cfg-def">Default: <code>10s</code></span>

How long a client may take to send its request. An outbound sends its request with the
first payload and waits for no answer, so it has nothing to bound and ignores this.

## Outbound

### `uuid`

<span class="cfg cfg-type">secret</span>
<span class="cfg cfg-req">Required</span>

The user's UUID, written as above. An outbound refuses `users`.

### `max_first_payload_delay`

<span class="cfg cfg-type">duration</span>
<span class="cfg cfg-def">Default: <code>400ms</code></span>

The longest the request waits for the application's first bytes, so the two travel in one
packet and a connection never opens with a small packet of a fixed length on its own. A
server-first protocol such as SSH or SMTP sends nothing until the server has spoken, so once
this passes the request goes alone. A longer wait delays the first byte of those protocols; a
shorter one sends the request alone more often. A written zero is refused.

</div>

## UDP

VLESS names the destination of a UDP stream once, in its request, and the datagrams after it
carry none. So an Apranik outbound opens one stream to the server for each destination a UDP
session sends to, and keeps it for the rest of the session. A session talking to a DNS
server and a game server holds two streams. There is no limit on how many a session may
hold, so a session that sends to very many destinations holds that many connections to the
server.

On an inbound, VLESS looks at the request's command byte to tell a TCP request from a UDP one
before accepting it, and answers replies on the stream they came from.

## Answer and fallback

The server's answer starts with a two-byte header (the version and no addons) which goes in
front of the first thing the server sends, never alone. A request that is not valid VLESS (an
unknown UUID, another version, or not VLESS at all) is handed to the inbound's
[`fallback`](../inbound/index.md#fallback) if one is set, with every byte it sent. A
request shorter than VLESS's header waits for more bytes until `handshake_timeout`, then is
closed.
