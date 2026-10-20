# Outbound groups

A group is an outbound that picks one of other outbounds, its members, for each
connection. Core builds it; it is not a proxy protocol and has no server of its own.
Routing sends traffic to the group by its `tag`, like to any outbound.

| Type | Picks | Measures |
| --- | --- | --- |
| [`urltest`](urltest.md) | the member whose probe answered fastest | yes |
| [`fallback`](fallback.md) | the first member in list order whose probe passed | yes |
| [`selector`](selector.md) | the member a person chose through an [API](../api/index.md) | no |

## Structure

<div class="cfg-structure">

```json
{
  "outbounds": [
    {
      "tag": "auto",
      "protocol": {
        "type": "urltest",
        "settings": { "members": ["server-a", "server-b"] }
      }
    },
    {
      "tag": "proxy",
      "protocol": {
        "type": "selector",
        "settings": { "members": ["auto", "server-a", "direct"] }
      }
    }
  ]
}
```

</div>

A group has only `tag` and `protocol`: no `server`, `transport`, `dns_server`,
`muxcool` or `proxy_request_timing`.

## Shared behaviour

- `members` lists outbound tags. A member may be another group, so a `selector`
  can choose between a `urltest` and a single server. `check` refuses a group that contains itself, directly or through other groups.
- A group carries only the networks every member carries: one TCP-only member makes
  the group TCP-only.
- A UDP session keeps the member it got first; existing connections never move.
- `urltest` and `fallback` measure members with a [probe](../shared/probe.md) that
  starts with `run`, never with `check`.
- [Statistics](../stats/index.md) count a connection under the group and under the
  member that carried it.
