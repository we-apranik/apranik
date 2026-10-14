# URLTest

An outbound group that uses the member whose probe answered fastest.

[![A urltest group: every interval it sends one timed HTTP request through each member. server-a is current at 120 ms; server-b answers in 100 ms, faster by less than the 50 ms tolerance, so the group stays on server-a; server-c failed its probe. A connection tries the current member, then the others that passed, fastest first, then the failed ones.](../../assets/diagrams/group-urltest.excalidraw.svg)](../../assets/diagrams/group-urltest.excalidraw.svg)

## Structure

<div class="cfg-structure" markdown>

```json
{
  "tag": "auto",
  "protocol": {
    "type": "urltest",
    "settings": {
      "members": ["server-a", "server-b"],
      "probe": {
        "url": "https://www.gstatic.com/generate_204",
        "interval": "5m",
        "timeout": "10s",
        "expected_status": "200-299"
      },
      "tolerance": "50ms"
    }
  }
}
```

</div>

A group has only `tag` and `protocol`: no `server`, `transport` or `dns_server`.

<div class="cfg-fields" markdown>

## Fields

### `members`

<span class="cfg cfg-type">list of tags</span>
<span class="cfg cfg-req">Required</span>

The outbounds to choose from, by tag. A member may be another group. The order
breaks ties.

### `probe`

<span class="cfg cfg-type">object</span>

How members are measured. See [Probe](../shared/probe.md).

### `tolerance`

<span class="cfg cfg-type">duration</span>
<span class="cfg cfg-def">Default: <code>50ms</code></span>

How much faster another member must be before the group switches to it. Without
it, two servers of similar speed would swap after every round.

</div>

## How it chooses

- After each round of probes, it switches to the fastest member that passed only if
  that member beats the current one by more than `tolerance`. If the current
  member failed, it switches at once.
- A connection tries the members in that order and uses the first that connects.
  A member whose probe failed is tried last.
- Until the first round ends, and when every probe fails, it keeps list order or
  its last good member.
- A UDP session keeps the member it got first.
- The group carries only the networks every member carries: one TCP-only member
  makes the group TCP-only.

An example with `tolerance` at `50ms`:

| Round | server-a (current) | server-b | Result |
| --- | --- | --- | --- |
| 1 | 120 ms | 100 ms | 20 ms faster: stays on server-a |
| 2 | 140 ms | 70 ms | 70 ms faster: switches to server-b |
| 3 | 90 ms | 100 ms | server-a 10 ms faster: stays on server-b |

Existing connections never move; only new ones use the new member.
