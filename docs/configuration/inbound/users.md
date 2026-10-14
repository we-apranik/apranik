# Users

A user is a name plus the credentials an inbound's protocol needs. The name is
reported for each connection, matched by the routing rule field `user`, and used to
count statistics.

## Where users are written

In the inbound protocol's `settings.users`:

| Protocol | Each user | With no users |
| --- | --- | --- |
| [Trojan](../protocol/trojan.md#users) | `name` and `password` | refused |
| [VLESS](../protocol/vless.md#users) | `name` and `uuid` | refused |
| [SOCKS5](../protocol/socks5.md#users) | `name` and `password` | no login |
| [HTTP](../protocol/http.md#users) | `name` and `password` | no login |
| [Mixed](mixed.md#users) | `name` and `password` | no login |

An outbound has no `users`; it takes its protocol's own credentials.

## Names

- Unique within one inbound. The same name on two inbounds is one person: their
  statistics add up.
- Any language. Control characters are refused in names and passwords.
- Each protocol refuses what it cannot carry: `:` in an HTTP name, more than 255
  bytes in SOCKS5.

## Changing users while running

An [API](../api/index.md), such as the [Clash API](../api/clash-endpoints.md#users), can add,
remove and list users without a reload.

- Changes apply from the next handshake. Removing a user ends their connections on
  that inbound.
- A password change is a removal, then an addition.
- Nothing is saved: a restart or reload reads the config file again.
- Removing the last user makes Trojan and VLESS refuse everyone, and SOCKS5, HTTP and
  mixed accept everyone.

Quotas, expiry and per-address limits are left to the panel that manages users.
