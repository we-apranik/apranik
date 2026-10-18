---
description: "Every endpoint of Apranik's Clash API: what each one reads or changes, with its request and answer."
---

# Clash API endpoints

Every request needs the [secret](clash.md#secret). Errors are
`{"message": "..."}` with an HTTP status; an unknown path answers 404.

## Read

| Endpoint | Answers |
| --- | --- |
| `GET /` | `{"hello": "clash"}` |
| `GET /version` | `{"meta": true, "version": "Apranik 1.2.3"}` |
| `GET /configs` | `mode` (always `rule`), `mode-list`, `log-level` |
| `GET /traffic` | bytes up and down in the last second, and totals, every second |
| `GET /memory` | memory in use, every second |
| `GET /connections` | the [live list](#connections) |
| `GET /proxies`, `GET /proxies/{name}` | the outbounds; groups have `now` and `all` |
| `GET /rules` | the routing [rules](#rules) and their hits |
| `GET /logs?level=` | the live log: `debug`, `info`, `warning`, `error` |
| `GET /logs/file` | the [log file](#log-file) |
| `GET /dns/query?name=&type=` | one lookup through the resolver |
| `GET /inbounds/{tag}/users` | an inbound's [user](#users) names |
| `GET /inbounds/{tag}/users/{name}` | whether a user exists |

`/traffic`, `/memory`, `/connections` and `/logs` stream over WebSocket, and the
first three also as JSON lines.

## Change

| Endpoint | Does |
| --- | --- |
| `DELETE /connections` | closes every live connection |
| `DELETE /connections/{id}` | closes one; a UDP entry ends its whole session |
| `PUT /proxies/{name}` | switches a [selector](../outbound/selector.md): `{"name": "<member>"}` |
| `GET /proxies/{name}/delay` | one delay test: `{"delay": 120}`, in milliseconds |
| `GET /group/{name}/delay` | a delay test of every member |
| `POST /cache/dns/flush` | forgets cached DNS answers |
| `POST /reload` | reloads the config file: 400 with every problem, or 202 |
| `POST /inbounds/{tag}/users` | adds a user |
| `DELETE /inbounds/{tag}/users/{name}` | removes a user and ends their connections |
| `POST /inbounds/{tag}/users:batchCreate` | adds many users |
| `POST /inbounds/{tag}/users:batchDelete` | removes many users |

Delay tests take `url`, `timeout` (milliseconds) and `expected`; what they leave out
comes from [`delay_test`](clash.md#delay_test). They go through the named outbound,
never through routing.

## Connections

Listed only while [`stats.tracking`](../stats/index.md#trackingenabled) is on;
otherwise 503. `rule` is where the router decided (`routing.rules[3]` or `default`),
`chains` the outbounds from member to group, and `metadata.host` the sniffed name
or the requested domain.

## Rules

```json
{
  "index": 1,
  "type": "route",
  "payload": "domain_suffix=ir port=443",
  "proxy": "direct",
  "hitCount": 12
}
```

`type` is the action, and `default` for the last entry. `payload` writes the match
with the config's field names.

## Log file

`GET /logs/file` serves the config's `file` log output; 404 without one.

| Parameter | Does |
| --- | --- |
| `output=N` | the N-th `file` output, from 0 |
| `tail=N` | the last N lines |
| `contains=text` | only lines containing the text |
| `follow=true` | keep sending what is appended |
| `download=true` | send as an attachment |

## Users

Changes the [users](../inbound/users.md) of a Trojan, VLESS, SOCKS5, HTTP or mixed
inbound. A user is written as in the protocol's `users`. A name in the path is one
escaped segment.

```sh
curl -X POST -H "Authorization: Bearer $SECRET" \
  -d '{"name": "alice", "password": "correct horse"}' \
  http://127.0.0.1:9090/inbounds/trojan-in/users
```

A batch runs its items in order, is not atomic, and answers one result per item.
Status codes: 404 unknown inbound or user, 400 invalid user, 409 name taken, 413 body
too large.

## Not built

`/providers`, `/restart`, `/upgrade`, `/script`, `/profile`, `/storage`.
