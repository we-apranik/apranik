# Timeouts

Every timeout is set beside what it bounds. Each layer bounds its own handshake,
and nothing adds an overall limit, so a stack's handshakes may together take the
sum of their timeouts.

## Handshakes

| What | Key | Default |
| --- | --- | --- |
| TCP connect, name resolution included | [`tcp`](../transport/tcp.md#dial_timeout) `dial_timeout` | `10s` |
| Next address of a domain | [`tcp`](../transport/tcp.md#next_address_delay) `next_address_delay` | `250ms` |
| TLS | [`tls`](../transport/tls.md#handshake_timeout) `handshake_timeout` | `10s` |
| WebSocket | [`ws`](../transport/ws.md#handshake_timeout) `handshake_timeout` | `10s` |
| HTTPUpgrade | [`httpupgrade`](../transport/httpupgrade.md#handshake_timeout) `handshake_timeout` | `10s` |
| PROXY protocol header | [`proxy_protocol`](../transport/proxy_protocol.md#timeout) `timeout` | `1s` |
| SOCKS5 | [`socks5`](../protocol/socks5.md#handshake_timeout) `handshake_timeout` | `10s` |
| HTTP proxy | [`http`](../protocol/http.md#handshake_timeout) `handshake_timeout` | `10s` |
| Trojan | [`trojan`](../protocol/trojan.md#handshake_timeout) `handshake_timeout` | `10s` |
| VLESS | [`vless`](../protocol/vless.md#handshake_timeout) `handshake_timeout` | `10s` |
| Mixed's first byte | [`mixed`](../inbound/mixed.md#first_byte_timeout) `first_byte_timeout` | `10s` |
| Sniffing | [`sniff`](../routing/sniffing.md#timeout) `timeout` | `300ms` |
| Proxy request held for the first payload | [`proxy_request_timing`](proxy-request-timing.md#max_delay) `max_delay` | `400ms` |

## Idle

| What | Key | Default |
| --- | --- | --- |
| TCP connection | [inbound](../inbound/index.md#tcp_idle_timeout) `tcp_idle_timeout` | `5m` |
| UDP session | [inbound](../inbound/index.md#udp_session_idle_timeout) `udp_session_idle_timeout` | `5m` |
| UDP routing decision | [inbound](../inbound/index.md#udp_routing_cache_entry_idle_timeout) `udp_routing_cache_entry_idle_timeout` | `1m` |
| UDP live-list entry | [stats](../stats/index.md#trackingudp_entry_idle_timeout) `tracking.udp_entry_idle_timeout` | `1m` |
| Freedom's UDP socket | [freedom](../outbound/freedom.md#natidle_timeout) `nat.idle_timeout` | `5m` |
| TCP keep-alive | [`tcp`](../transport/tcp.md#keep_alive) `keep_alive` | `15s` |

## Other

| What | Key | Default |
| --- | --- | --- |
| One DNS query | [DNS server](../dns/server.md#settingstimeout) `settings.timeout` | `5s` |
| One group probe | [probe](probe.md#timeout) `timeout` | `10s` |
| Between probe rounds | [probe](probe.md#interval) `interval` | `5m` |
| Held UDP session (XUDP) | [Mux.Cool](muxcool.md#udpxudpglobal_idhold_time) `hold_time` | `60s` |
