# Timeouts

Every timeout is configured beside the component it bounds; there is no central
timeouts section. Durations are written as quoted strings such as `"10s"`.

[![Every timeout: for a TCP connection in order, inbound handshake_timeout 10s (overlays and proxy together), sniff action timeout 300ms (only when a rule sniffs), outbound dial_timeout 10s, outbound handshake_timeout 10s, then relay until tcp_idle_timeout 5m of silence; TCP keep-alive 15s and the -fetch-timeout flag. For a UDP session: udp_session_idle_timeout 5m, NAT idle_timeout 5m, resolver 5s with a 30s cache, and the internal 1m cached route.](../assets/diagrams/timeouts.excalidraw.svg)](../assets/diagrams/timeouts.excalidraw.svg)

| Timeout | Where | Default |
| --- | --- | --- |
| Client handshake, overlays included | [`inbounds[].handshake_timeout`](inbound/index.md#handshake_timeout) | `10s` |
| Sniffing wait | a `sniff` rule's [`settings.timeout`](routing/sniffing.md#timeout) | `300ms` |
| TCP idle | [`inbounds[].tcp_idle_timeout`](inbound/index.md#tcp_idle_timeout) | `5m` |
| UDP session idle | [`inbounds[].udp_session_idle_timeout`](inbound/index.md#udp_session_idle_timeout) | `5m` |
| Dial, overlays included | [`outbounds[].dial_timeout`](outbound/index.md#dial_timeout) | `10s` |
| Server handshake | [`outbounds[].handshake_timeout`](outbound/index.md#handshake_timeout) | `10s` |
| Trojan request wait for the first payload | internal | `100ms` |
| Fallback dial | internal | `10s` |
| `ws` handshake with early data, `httpupgrade` response with `pipeline` (sent on the first write, outside the dial) | internal | `10s` |
| `ws` Close frame on close | internal | `1s` |
| NAT socket idle | freedom `settings.nat.idle_timeout` | `5m` |
| TCP keep-alive | `tcp` settings `keep_alive` (for freedom: `settings.tcp.keep_alive`) | `15s` |
| TCP keep-alive probes | `tcp` settings `keep_alive_interval` and `keep_alive_count` | `15s`, `9` |
| UDP domain lookup | freedom `settings.udp.resolve_timeout` | `5s` |
| UDP lookup cache | freedom `settings.udp.resolve_cache_ttl` | `30s` |
| Remote config file download | `apranik run/check -fetch-timeout` | `30s` |

Two stay internal: a cached UDP route is forgotten after 1 minute of silence, and
the accept loop pauses 10ms after a failed accept.
