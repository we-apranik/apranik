# Stats

Apranik counts the traffic it carries: bytes and connections per inbound, per outbound
and per user, why connections fail, which routing rule decided each connection, the
resolver's work, and, when asked, the connections that are open now. The core only
counts. Reading the numbers is done by whatever sits on top: an embedding program calls
the functions below, and the `apranik` binary offers them through an [API](api.md), the
[Clash API](api/clash.md).

Counting is **on by default** and needs no configuration. The `stats` section switches
it off, narrows it, or turns on the two things that cost extra.

## Structure

<div class="cfg-structure" markdown>

```json
{
  "stats": {
    "enabled": true,
    "inbounds": ["socks-in"],
    "outbounds": ["server-a", "auto"],
    "users": ["alice"],
    "dial_health": true,
    "tracking": {
      "enabled": true,
      "udp_entry_idle_timeout": "1m",
      "udp_max_entries_per_session": 1024
    }
  }
}
```

</div>

<div class="cfg-fields" markdown>

## Fields

| Key | Format | Default |
| --- | --- | --- |
| `enabled` | `true` or `false` | `true` |
| `inbounds` | inbound tags to count | all |
| `outbounds` | outbound tags to count | all |
| `users` | user names to count | all |
| `dial_health` | `true` or `false`: time each outbound's dials | `false` |
| `tracking` | the live list, see [below](#tracking) | on when an [API](api.md) is configured, otherwise off |

</div>

A list narrows counting to the names in it; an empty or absent list means everything of
that kind. `apranik check` refuses an inbound or outbound tag that does not exist.
A user name is not checked: core learns a user only when a protocol authenticates one.
With `"enabled": false` the lists, `tracking` and `dial_health` are ignored and
`check` warns about them.

Memory stays fixed by the config and never grows with traffic, with two exceptions that
follow what is asked for. Users are counted as they authenticate. Each open TCP connection
costs about 250 bytes while `tracking.enabled` is on (measured: 10 000 hold about 2.5 MB),
and nothing when it is off. A UDP destination listed as live costs about 490 bytes, and a
remembered routing decision about 230 (both measured, 10 000 each). A UDP session lists at most
[`tracking.udp_max_entries_per_session`](#tracking)
destinations (1024 by default) and remembers a routing decision for at most
[`udp_routing_cache_max_entries_per_session`](inbound/index.md#udp_routing_cache_max_entries_per_session)
(1024 by default); a client that sends to more destinations than that has the ones used
longest ago forgotten, so memory per session is bounded: with the defaults at most about
0.5 MB of live entries and 0.24 MB of routing decisions, reached only by a client that talks
to over a thousand destinations at once (a normal session holds a few dozen). Nothing else is
kept per destination.

## Tracking

`tracking` is the live list: every TCP connection and every UDP entry open now, kept so they can be
listed, closed and attributed to online users. Counting does not need it; see
[Live conversations and online users](#live-conversations-and-online-users) for what it enables.
A UDP **entry** is one destination inside a UDP session; the limits below say whether they count
entries or sessions.

| Key | Format | Default |
| --- | --- | --- |
| `tracking.enabled` | `true` or `false` | on when an [API](api.md) is configured, otherwise off |
| `tracking.udp_entry_idle_timeout` | duration, greater than zero: one entry leaves the list after this much silence in either direction (the UDP session goes on) | `1m` |
| `tracking.udp_max_entries_per_session` | integer, greater than zero: how many entries one UDP session lists at once; the one used longest ago ends when it is full | `1024` |

Leaving a key out gives its default. A written zero is refused, never read as the default.
`apranik check` warns when the two UDP keys are set while tracking is off, when
`udp_entry_idle_timeout` is longer than an inbound's `udp_session_idle_timeout` (its session ends
first), and when an API is configured with `tracking.enabled` false (the API cannot list or close
connections or show online users). With `"enabled": false` for the whole section, tracking is off.

## What is counted

Each inbound, outbound and user has its own numbers, and **TCP and UDP are counted
apart**: a reader gets each network's numbers and their sum.

| Number | Meaning | Whose |
| --- | --- | --- |
| uplink bytes | payload from the client toward the destination | all, TCP and UDP each and together |
| downlink bytes | payload from the destination back to the client | all, TCP and UDP each and together |
| active connections | TCP connections open now | all, TCP |
| total connections | TCP connections served since start | all, TCP |
| UDP packets | datagrams carried, uplink and downlink | all, UDP |
| UDP sessions | UDP sessions started since start; for an outbound, sessions that opened a datagram through it | all, UDP |
| dropped datagrams | UDP datagrams lost, by reason (below) | inbound and outbound, UDP |
| refused connections | closed without reaching an outbound, by reason (below) | inbound |
| authentication failures | a client spoke the protocol and its credentials were refused | inbound |
| fallback connections | handed to the inbound's [fallback](inbound/index.md); the connections only, not their bytes | inbound |
| dial failures | outbound connections that could not be made, by reason (below) | outbound |
| timed dials, total and last dial time | with `dial_health` only; dials that connected | outbound |

Bytes are **payload**: what the user's traffic carries, without TLS, WebSocket or proxy
framing. The same traffic gives the same number whatever transport stack is under it.

A user is counted only after the protocol has authenticated them, so a scanner cannot
create counters. A protocol that does not authenticate (SOCKS5 with no password) counts
no users.

## Why connections fail

The reasons are a fixed list, never text a peer sent, so counting by reason costs the
same memory whatever the traffic. A reader sees only the reasons that happened.

| Refused connection (inbound) | Meaning |
| --- | --- |
| `max_tcp_connections` | the inbound was at its `max_tcp_connections` |
| `max_udp_sessions` | a UDP session was requested while the inbound was at its `max_udp_sessions` |
| `transport_handshake_failed` | a transport layer such as TLS or WebSocket failed its handshake |
| `proxy_handshake_failed` | the proxy protocol's handshake failed, and no fallback served it |
| `handshake_timed_out` | a layer's handshake hit its deadline: a client too slow or silent |
| `blocked_by_rule` | a routing `block` action |
| `fallback_unreachable` | the request was meant for the fallback, which could not be reached |
| `other` | anything else |

**Authentication failures** are counted apart from refusals, and whatever happens to the
connection next: a Trojan client with a wrong password that is handed to the fallback is
one authentication failure and one fallback connection, and not a refusal. Garbage that
is not the protocol at all is not an authentication failure. A wave of them on one inbound
is what guessing passwords looks like. SOCKS5, HTTP and Trojan report them.

| Dial failure (outbound) | Meaning |
| --- | --- |
| `timeout` | a deadline passed |
| `refused` | the server refused the connection |
| `unreachable` | no route to the network or host |
| `reset` | the connection was reset |
| `dns_failed` | the name did not resolve |
| `handshake_failed` | a TLS handshake failed: an alert from the server or a certificate that did not verify |
| `canceled` | the client left before the dial finished |
| `other` | anything else, including a proxy refusing the request in its own protocol |

A group's dial failure is counted on the group, and not on each member it tried. Probe
dials never reach this count. A failure to create a UDP datagram through an outbound is
a dial failure too.

## UDP

UDP numbers lag a little. A session counts its datagrams for itself and adds them to the
inbound, user and outbound numbers every 64 datagrams, about once a second, and when it ends,
so that many busy sessions do not fight over the same counters. A read taken during traffic is
therefore behind by up to a second and 63 datagrams for each direction of each session, and is
exact once the session has ended. The live entries' own bytes are not delayed.

A UDP session may send to many destinations through many outbounds, and has no end but
silence. So there is no "active UDP sessions" number; with `tracking.enabled` on, the live
list shows what is open. Bytes are payload, counted when the outbound accepted a
datagram (uplink) and when one was received from the outbound (downlink). A reply that
was received and then lost is still downlink, and also a drop.

A datagram that does not reach the other side is counted **once, where it was lost**, by
a fixed reason, never per user. Only the reasons that happened are listed.

| Dropped on the inbound's side | Meaning |
| --- | --- |
| `blocked_by_rule` | the destination's routing decision is a `block`; counted for every packet |
| `routing_failed` | routing the destination returned an error |
| `outbound_cannot_carry_udp` | the chosen outbound has no UDP |
| `too_large_from_client` | the client's datagram exceeded [`udp_max_datagram_size`](inbound/index.md#udp_max_datagram_size) |
| `session_ended` | the datagram arrived as the session ended |
| `other` | anything else |

| Dropped on an outbound's side | Meaning |
| --- | --- |
| `outbound_open_failed` | the outbound's datagram could not be created |
| `send_failed` | writing to the destination failed |
| `too_large_from_outbound` | a reply was bigger than the maximum size: the session's [`udp_max_datagram_size`](inbound/index.md#udp_max_datagram_size) or the NAT's [`max_datagram_size`](outbound/freedom.md#natmax_datagram_size) |
| `reply_not_delivered` | a reply could not be written back to the client, or the session ended before it could be |
| `rejected_by_nat` | a reply that [freedom's NAT filtering](outbound/freedom.md) dropped: it came from an address the session never sent to. Spoofing and scanning show up here; the log only samples them |
| `other` | anything else |

A group counts a datagram it lost on the group, not on a member; a reply the NAT rejects
is counted on the freedom outbound that owns the NAT, which is a member when freedom sits
under a group. A rejected reply is not a received one: it counts as no downlink.

## Which rule decided

Every connection counts one hit on the routing rule that decided it, named by where the
rule sits in the config: `routing.rules[0]`, `routing.rules[1]`, ... and `default`. A
rule has no name of its own. A rule that never matched is listed with zero, so a reader
sees which rules do nothing. A `block` action counts a hit too.

A UDP session routes each destination, so a hit is **one routing decision**, not one
packet: a destination counts one hit, and counts again if its decision was forgotten after
[`udp_routing_cache_entry_idle_timeout`](inbound/index.md#udp_routing_cache_entry_idle_timeout) of silence.

So the same traffic can count different hits under different settings. For example, a client
that sends to `1.1.1.1:443` every 90 seconds counts a hit on every packet with the default
1 minute, and one hit for the whole stretch with `5m`. Read a UDP rule's hits as "how many
times this rule decided a destination", and use the bytes and packets counted on the
inbound and outbound for how much traffic it was.

## Dial health

With `"dial_health": true`, the handler times each dial of the outbound routing named.
A reader gets the number of dials that connected, their total time and the last one's;
the mean is the total over the number. A group's time is the whole dial, members it tried
first included. It is off by default because it reads the clock around every dial.

## DNS

The resolver counts its own work, per server tag (the `system` server included):

| Number | Meaning |
| --- | --- |
| lookups | names asked of the resolver; an address literal and `localhost` never reach a server and are not counted |
| cache hits, stale answers | record types answered from the cache, so a name looked up for both address families can count two |
| failures by reason | `timeout`, `no_answer` (the name has no address), `truncated`, `server_failed` (an error response code), `other` |
| per server: queries, failures, total latency | exchanges with that server; a cache hit makes none; the mean is total latency over queries |

## Groups

A connection through an [outbound group](outbound/group.md) is counted under the group
**and** under the member that carried it. A group's numbers therefore include its
members', so a reader adding up traffic across outbounds must add only the entries
where `IsGroup` is false, or it counts the same bytes twice. Probe requests are not
counted.

## Live conversations and online users

With `tracking.enabled` on, each TCP connection is listed from the moment it is dialed
until it ends, with its inbound, user, source address, destination, the outbounds that
carried it (the group, then the member), the rule that decided it, when it began and its
own bytes. A connection still in its handshake or dial is not listed. Each connection has
a number, the next in a sequence, shown as text; a larger number began later.

A UDP session is listed **one entry per destination**, from the destination's first
datagram or reply. It ends after [`tracking.udp_entry_idle_timeout`](#tracking)
of silence in either direction (swept every quarter of that time), or when the session ends, or
when the session lists more than
[`tracking.udp_max_entries_per_session`](#tracking)
and it is the one used longest ago. It does not depend on the routing cache: forgetting a
routing decision leaves the entry. A reply from an address that is not a listed destination (or
that two domains of the session share) counts on the inbound, outbound and user but on no entry.

A live conversation can be **closed** by an [API](api.md). A TCP connection ends. Closing a UDP entry ends
the **whole UDP session** it belongs to, every destination of it, because the relay and the session
can only end together; closing just one entry would change nothing, since the next datagram to that
destination would list a new one. A close is a request: it returns at once, and the entry leaves the
list a moment later. With `tracking.enabled` off nothing is listed and nothing can be closed; the
answer says so.

A user is **online** when at least one live conversation is theirs, a live UDP entry
included. A UDP entry stays up to a quarter more than `tracking.udp_entry_idle_timeout` after
the last datagram or reply, so a user is online for that long after their last UDP packet; a panel
that limits IPs should allow for the lag. For example, a user whose phone moves from home wifi to
mobile data stops sending from the old address, but the old address stays listed for up to
75 seconds with the default `1m`, so a limit of two addresses sees three for that long. A panel
that bans on the first sighting would ban them; one that waits a minute or two does not.
Lower the setting to shorten the lag, at the cost of more entries coming and going. Each online user comes
with the distinct source addresses of those connections, and for each address when its
newest live conversation began. The list is built when asked, from the live conversations,
so it costs nothing while nobody asks. **Nothing is kept once the last connection from an
address ends**: there is no history of who connected from where. A protocol that does
not authenticate has no users, so there are none to list.

The registry tracks every connection whatever the `inbounds` list says, so the list is
complete. The total of all traffic counts only the inbounds that are counted.

## Reading the numbers

Each kind of data has its own read, so a reader pays only for what it asks for:

| Read | Returns | Size |
| --- | --- | --- |
| `StartedAt()` | when this process began counting | one value |
| `TotalTraffic()` | uplink and downlink bytes of every counted inbound, all, TCP and UDP | three pairs, no allocation |
| `InboundTraffic()` | numbers per inbound tag | the inbounds |
| `OutboundTraffic()` | numbers per outbound tag, groups marked | the outbounds |
| `UserTraffic()` | numbers per authenticated user | grows with the users |
| `RuleHits()` | hits per rule location | the rules |
| `DNS()` | the resolver's numbers | the DNS servers |
| `LiveConversations()` | the connections open now, oldest first | grows with open connections |
| `OnlineUsers()` | users with a live conversation, and their addresses | grows with open connections |

`Builder.Stats()` gives the registry; every read is a copy, so any number of readers can
read at once. Counters never reset and nothing is written to disk. They count from the
moment the process started, and active connections is the only number that goes down. To
get the traffic between two moments, read twice and subtract. If `StartedAt()` differs
between them, the process restarted and the counters began again from zero. A read taken
during traffic is not one instant: each counter is read in turn. Keeping history across
restarts is up to whatever reads.
