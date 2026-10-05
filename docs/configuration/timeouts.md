# Timeouts

Every timeout is configured beside the component it bounds; there is no central
timeouts section. Durations are written as quoted strings such as `"10s"`.

Every layer bounds its own handshake with the timeout in its own settings, so
each can be set on its own: `tcp`'s `dial_timeout`, then each overlay's and the
protocol's `handshake_timeout`. Nothing above them adds an overall limit, so a
stack's handshakes can together take the sum of their timeouts.

| Timeout | Where | Default |
| --- | --- | --- |
| TCP connect, name resolution included | [`tcp`](transport/tcp.md#dial_timeout) settings `dial_timeout` | `10s` |
| TLS handshake | [`tls`](transport/tls.md#handshake_timeout) settings `handshake_timeout` | `10s` |
| WebSocket upgrade, early data included | [`ws`](transport/ws.md#handshake_timeout) settings `handshake_timeout` | `10s` |
| HTTPUpgrade upgrade, `pipeline` included | [`httpupgrade`](transport/httpupgrade.md#handshake_timeout) settings `handshake_timeout` | `10s` |
| SOCKS5 handshake | [`socks5`](protocol/socks5.md#handshake_timeout) settings `handshake_timeout` | `10s` |
| HTTP proxy request | [`http`](protocol/http.md#handshake_timeout) settings `handshake_timeout` | `10s` |
| Trojan request (inbound) | [`trojan`](protocol/trojan.md#handshake_timeout) settings `handshake_timeout` | `10s` |
| PROXY protocol header | [`proxy_protocol`](transport/proxy_protocol.md#timeout) settings `timeout` | `1s` |
| Mixed's first byte | [`mixed`](inbound/mixed.md#first_byte_timeout) settings `first_byte_timeout` | `10s` |
| Sniffing wait | a `sniff` rule's [`settings.timeout`](routing/sniffing.md#timeout) | `300ms` |
| TCP idle | [`inbounds[].tcp_idle_timeout`](inbound/index.md#tcp_idle_timeout) | `5m` |
| UDP session idle | [`inbounds[].udp_session_idle_timeout`](inbound/index.md#udp_session_idle_timeout) | `5m` |
| Trojan request wait for the first payload | internal | `100ms` |
| Fallback dial | internal | `10s` |
| `ws` Close frame on close | internal | `1s` |
| NAT socket idle | freedom `settings.nat.idle_timeout` | `5m` |
| TCP keep-alive | `tcp` settings `keep_alive` (for freedom: `settings.tcp.keep_alive`) | `15s` |
| TCP keep-alive probes | `tcp` settings `keep_alive_interval` and `keep_alive_count` | `15s`, `9` |
| One DNS query | each [DNS server](dns/index.md)'s `settings.timeout` | `5s` |
| One group probe | an [outbound group](outbound/group.md#probes)'s `probe.timeout` | `10s` |
| Time between group probe rounds | an [outbound group](outbound/group.md#probes)'s `probe.interval` | `5m` |
| A domain's next address | `tcp` settings `next_address_delay` | `250ms` |
| Remote config file download | `apranik run/check -fetch-timeout` | `30s` |

Two stay internal: a cached UDP route is forgotten after 1 minute of silence, and
the accept loop pauses 10ms after a failed accept.
