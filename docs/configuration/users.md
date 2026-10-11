# Users

A user is one person who may use an inbound: a name, and the credentials the inbound's
protocol needs. The name is what Apranik knows them by: it is reported as the user of each
connection, routing rules can match it (`user`), and statistics are counted under it.

## Where users are written

Users belong to an inbound and are written in its protocol's settings, as `users`. The
credentials differ by protocol, so each protocol checks its own.

| Inbound protocol | `users` | With none |
| --- | --- | --- |
| [Trojan](protocol/trojan.md#users) | `{ name, password }`, at least one | refused at start |
| [VLESS](protocol/vless.md#users) | `{ name, uuid }`, at least one | refused at start |
| [SOCKS5](protocol/socks5.md#users) | `{ name, password }`: the RFC 1929 username and password | no login: everyone is accepted |
| [HTTP](protocol/http.md#users) | `{ name, password }`: Basic authentication | no login: everyone is accepted |
| [Mixed](inbound/mixed.md#users) | `{ name, password }`, one list for SOCKS5 and HTTP | no login: everyone is accepted |

An outbound has no users: it logs in to its server with the one `password`, or `username` and
`password`, that its protocol takes. An inbound refuses an outbound's keys and an outbound
refuses `users`, so a key written for the wrong side is an error and never an open proxy.

## Names

A name is unique inside one inbound. The same name on two inbounds is the same person: their
statistics add up, and a rule matching the user matches both. Names and passwords may be in any
language. A control character (a newline, a NUL, an escape) is refused in both, because HTTP
Basic authentication forbids it and a name is written to logs. Anything else a protocol cannot
carry is that protocol's error, and `apranik check` reports it: HTTP names cannot contain
`:`, and SOCKS5 names and passwords are at most 255 bytes.

A user of [mixed](inbound/mixed.md) has to be valid for both protocols, since one list serves
both.

## Changing users while running

A protocol whose users can change implements Go's `spec.UserManager`, and Trojan, VLESS, SOCKS5, HTTP
and mixed do. A program that embeds Apranik, and an API module such as the
[Clash API](api/clash.md#users), can add a user, remove one and list the names without a reload. The
change applies from the next handshake; connections already accepted keep going until they end,
or until core ends them when the user is removed.

- **A password change** is a removal followed by an addition. The user is refused in between, and
  their connections on that inbound end with the removal.
- **Nothing is saved.** A user added while running is gone after a restart or a successful reload,
  which read the config file again. A reload that fails starts the previous configuration again
  as it was, users added while running included. A panel that manages users sends them again after
  a reload.
- **The last user.** Trojan and VLESS refuse every client until one is added. SOCKS5, HTTP and mixed then
  have no users, which means no login: they accept everyone. Removing the last user removes
  authentication, and adding a user to an inbound with none makes it ask for a login.
- **Statistics.** A user is counted from their first connection, and the counters of a removed
  user stay (counters never reset). See [Stats](stats.md).

## Not here

Quotas, expiry dates, limits per address and similar policies belong to whatever manages the
users, as with Xray and sing-box. Apranik counts what each user carries; it does not decide how
much they may.
