# Clash API

The Clash API lets a dashboard (yacd, MetaCubeXD, zashboard) or an app that speaks it read
and drive a running Apranik: traffic, open connections, outbounds, the log, the choice of a
selector's server. It follows the shapes of [Mihomo](https://github.com/MetaCubeX/mihomo)'s
API where Apranik has the same thing, and says so below where it does not.

It is an [API module](../api.md): `"type": "clash"` in the `apis` list, with its own
listener. It sees the core only through the provider core gives every API.

## Structure

<div class="cfg-structure" markdown>

```json
{
  "apis": [
    {
      "type": "clash",
      "settings": {
        "listen": "127.0.0.1",
        "port": 9090,
        "secret": "a long random string",
        "allow_origins": ["https://metacubex.github.io"],
        "log_buffer_records": 256,
        "delay_test": {
          "url": "https://www.gstatic.com/generate_204",
          "timeout": "10s",
          "expected_status": "200-299"
        }
      }
    }
  ]
}
```

</div>

<div class="cfg-fields" markdown>

## Fields

| Key | Format | Default |
| --- | --- | --- |
| `listen` | an IP address | required |
| `port` | 1 to 65535 | required |
| `secret` | text | required |
| `allow_origins` | list of `scheme://host[:port]`, or `"*"` | none: no browser page may call the API |
| `external_ui` | an absolute path to a directory | none: no `/ui` |
| `log_buffer_records` | whole number, 1 to 100000 | `256` |
| `delay_test` | `url`, `timeout`, `expected_status`, each optional | see [delay tests](#delay-tests) |

</div>

A written `0` is refused for `log_buffer_records` and for `delay_test.timeout`, as everywhere
.

### `listen`, `port` and `secret`

There is no default address or secret: an API that listened on every interface, or answered
without a secret, because a line was left out would be an exposure. Every request must carry
the secret as `Authorization: Bearer <secret>`. A WebSocket handshake, which a browser cannot
give headers, may carry it as `?token=<secret>` instead; the query string is refused on any other
request because a query string is logged and kept in history.

`apranik check` warns when `listen` is not a loopback address. Loopback is not private on a
phone: any app on it can reach the port, so keep the secret long.

### `allow_origins`

A browser lets a page call the API, or open a WebSocket to it, only when the API says the page's origin
is allowed. List the origins of the dashboards you use. `"*"` allows every page, and `check` warns about it: a page
still needs the secret, but the secret then sits in a page you do not control. Calls that are
not from a browser (curl, an app) are not affected.

## Endpoints

All of them need the secret. Errors are always `{"message": "..."}` with the status, except a refused
WebSocket handshake, which answers with the WebSocket library's short reply. A path that is not
listed answers 404.

### Read

| Endpoint | Answers |
| --- | --- |
| `GET /` | `{"hello": "clash"}` |
| `GET /version` | `{"meta": true, "version": "Apranik 1.2.3"}`; `meta` tells dashboards the Meta-family endpoints exist |
| `GET /configs` | `mode` (always `rule`), `mode-list`, `log-level`; no ports: the ports are in the config file |
| `GET /traffic` | `{"up", "down", "upTotal", "downTotal"}` every second, the bytes of the last second and the totals |
| `GET /memory` | `{"inuse", "oslimit"}` every second: the memory the process holds from the system, and no limit |
| `GET /connections` | the live conversations, see [below](#connections) |
| `GET /proxies`, `GET /proxies/{name}` | every outbound; groups have `now` and `all`; see [below](#proxies) |
| `GET /rules` | the routing rules, see [below](#rules) |
| `GET /logs?level=` | the log as it is written, see [below](#logs) |
| `GET /logs/file` | the log file, see [below](#the-log-file) |
| `GET /dns/query?name=&type=` | one lookup through the resolver |
| `GET /inbounds/{tag}/users`, `GET /inbounds/{tag}/users/{name}` | an inbound's [users](#users): all the names, or whether one exists |

`/traffic`, `/memory`, `/connections` and `/logs` stream when the request is a WebSocket
upgrade (messages are text frames) and, for `/traffic`, `/memory` and `/logs`, otherwise as
chunked JSON lines.

### Change

| Endpoint | Does |
| --- | --- |
| `DELETE /connections` | closes every live conversation |
| `DELETE /connections/{id}` | closes one |
| `PUT /proxies/{name}` with `{"name": "<member>"}` | switches a [selector](../outbound/group.md#selector); 204 |
| `GET /proxies/{name}/delay?url=&timeout=&expected=` | one delay test, `{"delay": ms}` |
| `GET /group/{name}/delay?url=&timeout=&expected=` | a delay test of every member, `{"<member>": ms}`; a member that failed is left out |
| `POST /cache/dns/flush` | forgets the cached DNS answers |
| `POST /reload` | loads the config again, see [below](#reload) |
| `POST /inbounds/{tag}/users`, `DELETE /inbounds/{tag}/users/{name}` | adds or removes one [user](#users) |
| `POST /inbounds/{tag}/users:batchCreate`, `POST /inbounds/{tag}/users:batchDelete` | adds or removes many [users](#users) in one request |

`PUT` and `PATCH /configs` answer 405 with a message: the mode and the ports are not settable
through the API. Nothing is a silent no-op.

### The dashboard

`external_ui` is a directory holding a dashboard's files (MetaCubeXD, yacd, zashboard: unpack a
release into it). They are served at `/ui/` (`/ui` redirects there) **without the secret**: the
page has to load before it can ask for the secret, and every API call it then makes carries it.
Only `GET` and `HEAD` are answered, and a symbolic link inside the directory cannot lead outside
it. The directory is opened when the API starts, so a missing one fails `run`, not `check`. The
path must be absolute. Apranik does not download dashboards.

### Not built

`/providers/*` (404: Apranik has no providers), `/restart`, `/upgrade`, the fake-IP
cache, `/script`, `/profile`, `/storage`. A dashboard page that needs one of them shows an
error or an empty view.

## Connections

A live conversation is one open TCP connection or one UDP destination of a UDP session. They
are listed only when [`stats.tracking`](../stats.md) is on, which it is by default once an
`apis` entry exists; without it `GET /connections` and the `DELETE`s answer 503 and say so.
Each entry has Clash's keys:

- `metadata.type` is the inbound's protocol (`Socks5`, `HTTP`, `Mixed`, `Trojan`).
- `metadata.host` is the name a [sniffer](../routing/sniffing.md) read from the payload when a
  rule asked for a sniff, else the domain the client sent. `destinationIP` is empty when the
  client sent a domain. `sniffHost` is the sniffed name alone. The destination is never rewritten.
- `chains` lists the outbound that carried it first and the group last.
- `rule` is where the router decided: `routing.rules[3]` or `default`. `rulePayload` is empty.
- `processPath` is empty and `dnsMode` is `normal`: Apranik does not know them.
- `inboundName` and `inboundUser` are the inbound's tag and the authenticated user.

Closing a UDP entry ends its whole UDP session.

## Proxies

`type` is `Direct` for freedom, `Selector`, `URLTest` and `Fallback` for groups, and the
protocol's name for a proxy (`Trojan`, `Socks5`). `history` is always empty: showing delays over
time is the dashboard's job, and the delay endpoints measure on demand.

A read-only group named `GLOBAL` lists every outbound, with the default outbound as `now`, for
dashboards that expect it. An outbound you tag `GLOBAL` replaces it. `PUT /proxies/GLOBAL`
and a `PUT` on any outbound that is not a selector answer 400 and say why: `urltest` and
`fallback` choose by themselves.

## Delay tests

A request may carry `url`, `timeout` (milliseconds) and `expected` (status codes and ranges,
`"204"`, `"200,204"` or `"200-299"`; `expected_status` is accepted too). What a request leaves
out comes from `delay_test`, and what that leaves out from the module's own defaults: `https://www.gstatic.com/generate_204`, `10s`, `200-299`. These are the
same values a [group's probe](../outbound/group.md) defaults to, written in both places because a
module cannot read core's; if you change one, change the other.

Answers: 404 for an unknown outbound, 400 for a bad parameter, 504 for a timeout, 503 for any
other failure (the message says which). A delay of under one millisecond is reported as `1`,
because dashboards read `0` as a failure. The request is never routed: it goes through the
outbound you name.

## Rules

`GET /rules` is Clash's envelope with Apranik's own content, because an Apranik rule is several
conditions at once and a Clash rule is one. Each entry:

```json
{"index": 1, "type": "route", "payload": "domain_suffix=ir port=443", "proxy": "direct", "hitCount": 12}
```

`type` is the action: `route`, `block`, `sniff` or `resolve`, and `default` for the last entry,
the default action. `payload` writes the conditions with the names of the config file: fields all
have to hold, the values of a field are alternatives (`domain_suffix=a.com,b.com`), `*` is a rule
with no condition, and `any(... | ...)`, `all(... | ...)` and `not(...)` hold nested conditions.
`proxy` is the outbound of a `route`, and the DNS server of a `resolve` that names one.
`hitCount` is how many routing decisions the rule made, and is left out when statistics are off.

## Logs

`GET /logs?level=` takes `debug`, `info` (default), `warning`, `error` or `silent`. Each line is
`{"type": "info", "payload": "message key=value ..."}`; `type` folds Apranik's eight levels into
Clash's four (notice is `info`, critical and above are `error`). One request is one
subscription, so it works whatever the `log` section says, and it ends with the request. A client
that reads too slowly loses records, and the stream says how many where it happened;
`log_buffer_records` is how far one client may fall behind.

### The log file

`GET /logs/file` is Apranik's own, for people and scripts, not for dashboards. It serves the file
output the config names, as written; the path never comes from the request. It answers 404 when the
`log` section has no `file` output.

| Parameter | Does |
| --- | --- |
| `output=N` | the N-th `file` output of the config, from 0 |
| `tail=N` | the last N lines (1 to 100000), read from the end, never the whole file |
| `contains=text` | only lines containing the text; a plain, case-sensitive substring, applied before `tail` |
| `follow=true` | keep the request open and send what is appended; only complete lines; starts again after a truncation or a rotation |
| `download=true` | an attachment |

With none of `tail`, `contains` or `follow` it is a normal file download: `Range` and
`Last-Modified` work.

!!! warning
    Whoever holds the secret can read every destination your users reached. The file is
    private to its owner on disk (`0600`); this endpoint widens who can read it.

## Reload

`POST /reload` loads the config again from the file or URL `apranik run` started with
. It checks first: a config with errors
changes nothing, and the answer is 400 with every problem. A good one answers 202 and is applied
after the answer: everything is closed and started again, this API included, so a dashboard
reconnects. The statistics count from zero afterwards. A second reload while one waits answers 409.

## Users

These routes change the [users](../users.md) of an inbound while it runs. They are an Apranik
extension (Mihomo has none) and work on an inbound whose protocol can change its users: Trojan,
SOCKS5, HTTP and [mixed](../inbound/mixed.md). `{tag}` is the inbound's tag.

| Route | Body | Answer |
| --- | --- | --- |
| `GET /inbounds/{tag}/users` | | `200 {"users": ["alice", "bob"]}`, in the order they were added |
| `GET /inbounds/{tag}/users/{name}` | | `200 {"name": "alice"}`, or `404` |
| `POST /inbounds/{tag}/users` | one user, a JSON object | `201 {"name": "alice"}` |
| `DELETE /inbounds/{tag}/users/{name}` | | `204` |
| `POST /inbounds/{tag}/users:batchCreate` | an array of users | `200 {"results": [...]}` |
| `POST /inbounds/{tag}/users:batchDelete` | an array of names | `200 {"results": [...]}` |

A user is written as the protocol writes it in its `users`, for example `{"name": "alice",
"password": "correct horse"}`; unknown keys are refused. A name goes in the path as one
escaped segment (`a%2Fb`, `%D8%B3%D8%A7%D8%B1%D8%A7`); a name made only of dots has to write them as `%2E`,
because clients resolve `.` and `..` as directories.

- **Status codes.** `404` for an unknown inbound, an inbound whose protocol cannot change its
  users, or an unknown user (the message says which); `400` for an invalid user, a body that is
  not what the route takes or an empty batch; `409` when the name is taken; `413` for a body over
  16 KiB (one user) or 8 MiB (a batch).
- **A batch** runs its items in order and answers `200` with one result for each; one that fails
  does not stop the rest, and nothing is undone: it is not atomic. A result is
  `{"index": 0, "name": "alice", "ok": true}` or `{"index": 1, "ok": false, "error": "..."}`.
  Only an unknown inbound fails the whole request.
- **Removing a user** ends their live conversations on that inbound, and only on it. With
  [live tracking](../stats.md#live-conversations-and-online-users) off the user is still removed and
  the open connections stay, which the log says.
- **Nothing is saved.** A user added here is gone after a restart or a successful
  [reload](#reload), which read the config file again; see [Users](../users.md#changing-users-while-running).
  A password change is a removal then an addition.
- **Every add and removal is logged** at Info with the inbound and the user's name, never the password.

```sh
curl -X POST -H "Authorization: Bearer $SECRET" -d '{"name":"alice","password":"correct horse"}' \
  http://127.0.0.1:9090/inbounds/trojan-in/users
curl -X POST -H "Authorization: Bearer $SECRET" -d '[{"name":"bob","password":"p1"},{"name":"carol","password":"p2"}]' \
  http://127.0.0.1:9090/inbounds/trojan-in/users:batchCreate
curl -X DELETE -H "Authorization: Bearer $SECRET" http://127.0.0.1:9090/inbounds/trojan-in/users/alice
```

## Examples

```sh
curl -H "Authorization: Bearer $SECRET" http://127.0.0.1:9090/proxies
curl -X PUT -H "Authorization: Bearer $SECRET" -d '{"name":"server-b"}' http://127.0.0.1:9090/proxies/choose
curl -H "Authorization: Bearer $SECRET" "http://127.0.0.1:9090/logs/file?tail=50&contains=refused"
curl -X POST -H "Authorization: Bearer $SECRET" http://127.0.0.1:9090/reload
```
