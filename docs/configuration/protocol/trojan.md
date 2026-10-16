# Trojan

TCP, and UDP framed inside the same stream. A request with a wrong password can go
to the inbound's [`fallback`](../inbound/index.md#fallback), so a probe sees an
ordinary site.

> [!WARNING]
> Trojan does not encrypt. Run it over [`tls`](../transport/tls.md), or the
> password hash and all traffic cross the wire in the clear.

## Structure

:::tabs
== Inbound

```json
{
  "tag": "trojan-in",
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
    "type": "trojan",
    "settings": {
      "users": [{ "name": "alice", "password": "correct horse" }],
      "handshake_timeout": "10s"
    }
  },
  "fallback": { "server": "127.0.0.1", "server_port": 8080 }
}
```

== Outbound

```json
{
  "tag": "trojan-out",
  "server": "proxy.example.com",
  "server_port": 443,
  "transport": [{ "type": "tcp" }, { "type": "tls" }],
  "protocol": {
    "type": "trojan",
    "settings": { "password": "correct horse" }
  }
}
```

:::

<div class="cfg-fields">

## Inbound fields

### `users`

<span class="cfg cfg-type">list of users</span>
<span class="cfg cfg-req">Required</span>

At least one, each with a `name` and a unique `password`. See
[Users](../inbound/users.md).

### `handshake_timeout`

<span class="cfg cfg-type">duration</span>
<span class="cfg cfg-def">Default: <code>10s</code></span>

How long a client may take to send its request.

## Outbound fields

### `password`

<span class="cfg cfg-type">string</span>
<span class="cfg cfg-req">Required</span>

The password sent to the server.

</div>

The client sends its request with the first payload; see
[Proxy request timing](../shared/proxy-request-timing.md). A UDP session uses one
stream for all its destinations.
