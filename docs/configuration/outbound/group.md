# Outbound groups

A group is an outbound that connects nowhere itself. It holds other outbounds, its
**members**, and picks one of them for each connection. [Routing](../routing/index.md)
points at a group by its tag, like at any outbound, and a [DNS server's
`outbound`](../dns/index.md) can be a group too.

There are two types, and they differ in how they choose:

| Type | Picks | Needs |
| --- | --- | --- |
| `urltest` | the member whose probe answered fastest | `probe`, and optionally `tolerance` |
| `fallback` | the first member, in list order, whose probe passed | `probe` |

A person choosing a member by hand (`selector`) is not built: it needs a way for a
client to tell the core which member to use, and there is none yet.

## Structure

<div class="cfg-structure" markdown>

```json
{
  "outbounds": [
    {
      "tag": "auto",
      "protocol": {
        "type": "urltest",
        "settings": {
          "outbounds": ["server-a", "server-b", "direct"],
          "probe": { "type": "https", "interval": "5m", "timeout": "10s" },
          "tolerance": "50ms"
        }
      }
    },
    { "tag": "server-a", "server": "203.0.113.5", "server_port": 443,
      "transport": [{ "type": "tcp" }, { "type": "tls" }],
      "protocol": { "type": "trojan", "settings": { "users": [{ "name": "me", "password": "secret" }] } } },
    { "tag": "server-b", "server": "198.51.100.7", "server_port": 443,
      "transport": [{ "type": "tcp" }, { "type": "tls" }],
      "protocol": { "type": "trojan", "settings": { "users": [{ "name": "me", "password": "secret" }] } } },
    { "tag": "direct", "protocol": { "type": "freedom" } }
  ]
}
```

</div>

A group takes none of the proxy outbound's other fields: no `server`, no
`transport`, no `dns_server`. A member has all of its own. A group may be listed
before its members, and a member may be another group; `check` refuses a group that
contains itself, directly or through other groups.

## How a connection is chosen

For every new connection the group tries its members in the order its probes favour,
and uses the first that connects. A member that fails between two probes costs that
one connection a retry on the next member, not an error. The connection is refused
only when every member failed, and the error lists each member's reason.

A member whose last probe failed is still tried, after the ones that passed. A failed
probe may be one lost packet, and refusing every connection because of it would be
worse than a slow attempt.

Until the first round of probes ends, which is right after the core starts, the group
uses list order.

A UDP session asks its group once and keeps the member it got, so a later probe never
moves a session to another server in the middle of its traffic.

A group carries only the networks **every** member carries. A group of a proxy that
carries TCP only and `freedom` carries TCP only. Otherwise UDP would leave directly
through `freedom` whenever the proxy was down, which is the kind of leak a group
should never cause. `check` warns when a rule sends UDP to a group that cannot carry it.

<div class="cfg-fields" markdown>

## Fields

### `outbounds`

<span class="cfg cfg-type">list of tags</span>
<span class="cfg cfg-req">Required</span>

The members, in the order that breaks ties and, for `fallback`, decides preference.
Each is the `tag` of another outbound. A group with one member never has anything to
pick, and `check` warns about it.

### `probe`

<span class="cfg cfg-type">object</span>
<span class="cfg cfg-req">Required</span>

How the group tests its members. See [Probes](#probes).

### `tolerance`

<span class="cfg cfg-type">duration</span>
<span class="cfg cfg-def">Default: <code>50ms</code></span>
<span class="cfg cfg-note">urltest only</span>

How much faster another member must be before `urltest` switches to it. See
[Tolerance](#tolerance-explained). `fallback` never compares speed, so `check`
refuses `tolerance` there.

</div>

## Probes

A probe is a small HTTP request sent **through** each member, to a URL you choose, and
timed. Each member is probed at the same time when the core starts, and again every
`interval`. A probe never goes through [routing](../routing/index.md): it uses the
member directly, so the result says something about that member alone.

The time measured runs from asking the member for a connection until the target's
status line and headers arrive. It includes every handshake a real connection pays: the
transport stack, the proxy protocol and, for `https`, TLS to the target. Measuring only
the connection would miss the proxy handshake, which a proxy client may send with the
first write.

A probe **passes** when the target answers with a `2xx` status. A redirect, an error
page, a timeout or a failed connection counts as failed.

The request carries no `User-Agent`, so it does not name Apranik or Go's HTTP client.

<div class="cfg-fields" markdown>

### `probe.type`

<span class="cfg cfg-type">string</span>
<span class="cfg cfg-req">Required</span>

| Value | Sends |
| --- | --- |
| `http` | a plain `GET` to the URL |
| `https` | the same `GET` after a TLS handshake with the target |

Use `https` where plain HTTP is filtered or rewritten. It costs a handshake per probe.
The TLS to the target uses Go's own `crypto/tls` (verified against the system's
certificate roots), not [`tls`'s `fingerprint`](../transport/tls.md). Through a proxy
member it travels inside the tunnel; through a `freedom` member it leaves directly.

### `probe.url`

<span class="cfg cfg-type">string</span>
<span class="cfg cfg-def">Default: <code>http://www.gstatic.com/generate_204</code>, or the <code>https://</code> one for the <code>https</code> type</span>

The target. Its scheme must match `probe.type`. Pick something that answers quickly
with a `2xx` and is reachable from wherever your members exit.

### `probe.interval`

<span class="cfg cfg-type">duration</span>
<span class="cfg cfg-def">Default: <code>5m</code></span>

The time between rounds, counted from the end of one to the start of the next.
Probing costs a few hundred bytes per member per round; a shorter interval notices a
failure sooner and shows the network a pattern more often.

### `probe.timeout`

<span class="cfg cfg-type">duration</span>
<span class="cfg cfg-def">Default: <code>10s</code></span>

How long one member may take, from the start of its probe to the status line. A member
that takes longer counts as failed.

</div>

## Tolerance explained {#tolerance-explained}

`tolerance` stops `urltest` from changing its mind over differences too small to
matter.

After every round, `urltest` compares the member it is using, the **current** one, with
the fastest member that passed. It switches only when the fastest is faster than the
current one by **more than `tolerance`**. If the current member's probe failed, it
switches at once, whatever the tolerance.

An example with `tolerance` set to `50ms`:

| Round | server-a (current) | server-b | What happens |
| --- | --- | --- | --- |
| 1 | 120ms | 100ms | `server-b` is faster by 20ms. That is not more than 50ms, so the group **stays** on `server-a`. |
| 2 | 130ms | 90ms | Faster by 40ms. Still within the tolerance: **stays**. |
| 3 | 140ms | 70ms | Faster by 70ms. More than 50ms: **switches** to `server-b`. |
| 4 | 90ms | 100ms | `server-b` is now current and `server-a` is faster by 10ms. **Stays** on `server-b`. |

Without a tolerance, two servers with similar speed would swap places every round, as
their timings jitter. Every swap sends **new** connections to a different exit server:
the websites you reach see a different address, long-lived logins may be asked again,
and a censor watching the traffic sees you move between servers. Existing connections
are never moved.

Choosing a value:

- **Larger** (for example `200ms`): the group is steadier and switches only for a
  clear improvement. A server that is slightly slower can keep the traffic for a long
  time.
- **Smaller** (for example `10ms`): the group follows the fastest server closely and
  may switch often when servers are alike.
- **`50ms`** is the default (PROVISIONAL). Only the difference between members
  matters, not how slow they all are, so a tolerance that suits one network may need
  tuning on another.

`tolerance` is a duration written as a string, such as `"50ms"`. Zero means the
default, and a value less than zero is refused. To make the group switch for any
improvement, write a very small value such as `"1ms"`.

When no probe passes in a round, `urltest` keeps the member it had, the last good one.
Before any round has finished it uses the first member in the list.

## When every probe fails

Neither type refuses connections because probes failed. `urltest` keeps its last good
member, and `fallback` uses list order. Each connection still tries the other members
if the first fails to connect, so the group degrades to retrying instead of stopping.

## What `check` reports

- a member tag that no outbound has, a group listing itself, or groups that choose
  among each other in a loop;
- a missing `probe`, an unknown `probe.type`, a `probe.url` whose scheme does not match
  the type, or a negative `interval`, `timeout` or `tolerance`;
- `tolerance` on a `fallback` group;
- `server`, `transport` or `dns_server` on a group;
- a DNS server whose outbound is a group with a member that needs the same DNS server
  for its own name, which is a lookup loop.

It warns about a group with one member, and about a rule that sends UDP to a group
with a member that cannot carry it.

`check` does not probe anything and dials nothing. Probing starts only when
[`apranik run`](../../cli.md) starts, and the logs say which member a connection used
(`Group member chosen`, at debug level) and when a group changes its choice
(`Group switched member`).
