# Counters

What each number in the [statistics](index.md) means. Counters start at zero when
the process starts, never reset and are never saved.

## Traffic

Kept per inbound, outbound and user, with TCP and UDP apart and summed.

| Counter | Meaning |
| --- | --- |
| uplink, downlink bytes | payload each way, without TLS, WebSocket or proxy framing |
| active connections | TCP connections open now |
| total connections | TCP connections since start |
| UDP packets | datagrams each way |
| UDP sessions | UDP sessions started |
| refused connections | inbound only, by [reason](#refusal-reasons) |
| authentication failures | inbound only: credentials refused |
| fallback connections | inbound only: handed to the [fallback](../inbound/index.md#fallback) |
| dial failures | outbound only, by [reason](#dial-failure-reasons) |
| dropped datagrams | by [reason](#udp-drop-reasons), where the datagram was lost |
| dial count and time | outbound only, with `dial_health` |

A user is counted only after authentication. A connection through a
[group](../outbound/urltest.md) counts under the group and the member, so add only
non-group outbounds for a total. UDP numbers lag by up to a second while a session
is busy.

## Refusal reasons

| Reason | Meaning |
| --- | --- |
| `max_tcp_connections` | the inbound was full |
| `max_udp_sessions` | the inbound had no room for another UDP session |
| `transport_handshake_failed` | TLS, WebSocket or another layer failed |
| `proxy_handshake_failed` | the proxy handshake failed, with no fallback |
| `handshake_timed_out` | a handshake hit its deadline |
| `blocked_by_rule` | a routing `block` |
| `fallback_unreachable` | the fallback could not be reached |
| `other` | anything else |

## Dial failure reasons

| Reason | Meaning |
| --- | --- |
| `timeout` | a deadline passed |
| `refused` | the server refused the connection |
| `unreachable` | no route |
| `reset` | the connection was reset |
| `dns_failed` | the name did not resolve |
| `handshake_failed` | a TLS handshake failed |
| `canceled` | the client left first |
| `other` | anything else, including a proxy refusing the request |

## UDP drop reasons

| Reason | Where | Meaning |
| --- | --- | --- |
| `blocked_by_rule` | inbound | the destination is blocked |
| `routing_failed` | inbound | routing returned an error |
| `outbound_cannot_carry_udp` | inbound | the outbound has no UDP |
| `too_large_from_client` | inbound | over [`udp_max_datagram_size`](../inbound/index.md#udp_max_datagram_size) |
| `session_ended` | inbound | arrived as the session ended |
| `outbound_open_failed` | outbound | the outbound's datagram could not be created |
| `send_failed` | outbound | writing to the destination failed |
| `too_large_from_outbound` | outbound | a reply over the size limit |
| `reply_not_delivered` | outbound | a reply could not reach the client |
| `rejected_by_nat` | outbound | a reply [freedom's NAT](../outbound/freedom.md#natfiltering) filtered out |
| `other` | both | anything else |

## Rule hits

One hit per routing decision on the rule that made it, named by position:
`routing.rules[0]`, … and `default`. A UDP session decides once per destination.

## DNS

Per server: lookups, cache hits, stale answers, queries, total latency, and failures
by reason: `timeout`, `no_answer`, `truncated`, `server_failed`, `other`.

## Live list and online users

With [`tracking`](index.md#trackingenabled) on, each open TCP connection and each
UDP destination is listed with its inbound, user, source, destination, outbounds,
rule, start time and bytes. Closing a UDP entry ends its whole UDP session.

A user is online while any entry is theirs. Their source addresses are listed, and
forgotten as soon as their last connection from an address ends.
