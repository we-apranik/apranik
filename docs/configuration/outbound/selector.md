# Selector

An outbound group whose member a person chooses through an [API](../api/index.md),
for example the Clash API's `PUT /proxies/{name}`.

[![A selector group: it has no probes. It starts on its first member and changes only when a person picks another through an API, such as the Clash API's PUT /proxies/{name}. A switch closes the live connections of the previous member, a failing chosen member fails the connection, and the choice is not saved.](../../assets/diagrams/group-selector.excalidraw.svg)](../../assets/diagrams/group-selector.excalidraw.svg)

## Structure

<div class="cfg-structure" markdown>

```json
{
  "tag": "proxy",
  "protocol": {
    "type": "selector",
    "settings": { "members": ["auto", "server-a", "server-b"] }
  }
}
```

</div>

<div class="cfg-fields" markdown>

## Fields

### `members`

<span class="cfg cfg-type">list of tags</span>
<span class="cfg cfg-req">Required</span>

The outbounds a person may choose, by tag. A member may be another group,
including another selector. The first is used at start.

</div>

## How it behaves

- It never measures and never switches by itself. `check` refuses `probe` and
  `tolerance`, and warns when no `apis` entry exists to change it.
- The choice is not saved: every start begins on the first member.
- When the chosen member fails, the connection fails.
- A switch closes the live connections that went through the previous member, when
  [`stats.tracking`](../stats/index.md#trackingenabled) is on. Otherwise only new
  connections move.
