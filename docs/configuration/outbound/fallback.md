# Fallback

An outbound group that uses the first member, in list order, whose probe passed.

[![A fallback group: every interval it probes each member. Members are listed in order of preference; server-a failed its probe, so connections use server-b, the first member that passed. server-a is used again as soon as its probe passes; members that failed are still tried, last.](../../assets/diagrams/group-fallback.excalidraw.svg)](../../assets/diagrams/group-fallback.excalidraw.svg)

## Structure

<div class="cfg-structure">

```json
{
  "tag": "failover",
  "protocol": {
    "type": "fallback",
    "settings": {
      "members": ["server-a", "server-b", "direct"],
      "probe": {
        "url": "https://www.gstatic.com/generate_204",
        "interval": "1m"
      }
    }
  }
}
```

</div>

A group has only `tag` and `protocol`: no `server`, `transport` or `dns_server`.

<div class="cfg-fields">

## Fields

### `members`

<span class="cfg cfg-type">list of tags</span>
<span class="cfg cfg-req">Required</span>

The outbounds to use, by tag, most preferred first. A member may be another group.

### `probe`

<span class="cfg cfg-type">object</span>

How members are tested. See [Probe](../shared/probe.md).

</div>

## How it chooses

- A connection tries members in list order, skipping those whose last probe failed,
  and uses the first that connects. Members that failed are tried last, not never.
- When every probe fails, it uses plain list order.
- A UDP session keeps the member it got first.
- The group carries only the networks every member carries: one TCP-only member
  makes the group TCP-only.
