# Performance log

What has been measured and changed, and what is still a candidate. The rules and the checklist are in the [performance guide](performance.md).

## Outbound group

Measured 2026-10-05 (`go test -bench GroupDialStream -benchmem -count 5 ./core/outbound/group`,
12 threads, medians). The members connect instantly, so the time is the group's own
choice of order. One allocation in the "after" column is the benchmark's own stub
stream.

| Benchmark | Before | After |
| --- | --- | --- |
| `urltest`, 3 members | 124 ns, 88 B, 3 allocs | 45 ns, 16 B, 1 alloc |
| `urltest`, 10 members | 248 ns, 256 B, 3 allocs | 45 ns, 16 B, 1 alloc |
| `fallback`, 3 members | 120 ns, 88 B, 3 allocs | 50 ns, 16 B, 1 alloc |
| `fallback`, 10 members | 238 ns, 256 B, 3 allocs | 50 ns, 16 B, 1 alloc |

"Before" built and sorted the member order for every connection; "after" works it
out once per probe round and a connection only reads it.

## Closing live conversations

Measured 2026-10-07 (`go test -bench HandleOneShortStreamCountingWithLiveConnections -benchmem -count 3 ./core/route/handler`).
A tracked connection runs under its own cancellable context so it can be closed from outside;
with live conversations off the handler makes none.

| | Before | After |
| --- | --- | --- |
| one short TCP connection, counting and live conversations on | 33-41 us, 9.5 KB, 57 allocs | 33-36 us, 10.0 KB, 62 allocs |
| the same with live conversations off | unchanged | unchanged |

## Logging hub

Measured 2026-10-07 (`go test -bench Disabled -benchmem -count 3 ./core/logging`). The hub is always in the chain; with no subscriber it
is disabled by one atomic read.

| | Before | After |
| --- | --- | --- |
| a log call below the configured level (one output, a derived logger) | 9-11 ns, 0 allocs | 14-17 ns, 0 allocs |
| `Enabled` at a disabled level | 4 ns, 0 allocs | 11 ns, 0 allocs |

The difference is the second handler the fan-out asks. It is paid by every log call below the output's level.

## Groups after the split

`go test -bench GroupDialStream -benchmem ./core/outbound/group`: unchanged, 100-110 ns, 80 B, 3 allocs for every type and size,
before and after `group` was split into `urltest`, `fallback` and the shared member set and prober.

## Done

Measured with the benchmarks above (`benchstat`, 5 runs each), 2026-09-27:

| Where | Change | Before → after, per operation |
| --- | --- | --- |
| `module/transport/base/udp` | `netip.AddrPort` end to end, `WriteToUDPAddrPort` | send to an IP: 5 → 0 allocations (the benchmark's own reader made the 6th) |
| `module/proxy/socks5` | address parsed in place; one reply buffer per association | read 4 → 0, write 4 → 0 allocations; write 357 → 104 ns |
| `module/proxy/trojan` | frame header peeked whole; one frame buffer reused under the write lock | read 5 → 0, write 3 → 0 allocations; write 365 → 89 ns |
| `shared/socksaddr` | `ParseAddress`, a slice-based decoder | 3 → 0 allocations, 86 → 22 ns |
| `core/net/udp/session` | networks checked once, when an outbound datagram opens | send 1 → 0 allocations |
| `core/net/udp/nat`, `core/net/udp/session` | replies read straight into pooled buffers (`core/net/udp`), released after use (`Reply.Release`) | 512 B and 1 allocation per reply → 0, in each |
| `spec` | `NewEndpointFromNetAddr` reads TCP and UDP addresses directly | 215 → 11 ns, 3 → 0 allocations |
| `core/inbound` | `max_tcp_connections`, `max_udp_sessions`: refuse beyond the limit before any handshake, or before a UDP session's socket is opened | new |
| `core/net/udp/nat`, `core/inbound` | refusal and rejection logs sampled once per 10 s (`logging.Sampler`) | a sprayer's 20 packets: 20 notices → 1 |
| `core/stats`, `core/relay` | payload bytes counted inside the idle-timeout reader, one atomic add per counter set per read (2026-10-05) | counting three counter sets: 25 ns per read on one goroutine, 174 ns with twelve goroutines sharing them and doing nothing else; per-connection counting flushed every 64 reads measured 2.4 ns and was not built. Loopback relay, 32 KiB writes: 15 to 22 µs counted and uncounted alike (bimodal, noisy). A short connection: 49 allocations uncounted, 51 counted. After the counters became methods with private fields (2026-10-06): 26 to 33 ns per read on one goroutine (a nil check per add), 170 ns shared across twelve goroutines, loopback bulk and short exchange unchanged within noise, 50 allocations for a counted short connection |
| `core/stats`, `core/net/udp/session`, `core/relay`, `core/route/handler` | UDP counting (2026-10-06) | `Session.Send` 142 ns without counting, 197 ns counting under three counter sets (inbound, user, outbound: two atomics each, bytes and packets), 0 allocations; a reply through the session 1185 ns without, 1280 ns counting; four counter sets cost 95 ns per datagram in isolation; a routing-cache hit (read lock, a clock read) 100 ns serial, 293 ns with twelve goroutines on one destination, 0 allocations; `TotalTraffic` now reads four values per inbound (about 30 ns for two inbounds, 0 allocations; it was 4 ns when it read two); a live entry after the smaller counters and sequence ID: 254 bytes (was 348), tracking and ending one 284 ns and 1 allocation (was 530 ns and 2), 1.4 µs per operation with twelve goroutines (was 2.2 µs), listing 10 000 takes 5.1 ms (was 14 ms). After the route cache became a bounded `shared/lru` (2026-10-06): a cache hit 128 ns serial (was 100: a hit now moves the entry to the front under one lock), about 500 ns with twelve goroutines hammering one destination of one session (was 293; a real session has two goroutines), 0 allocations; adding a route at the limit 215 ns and one allocation; the idle sweep stops at the first route that is not idle, so it costs what it removes. UDP live entries are their own bounded `shared/lru` per session (a lock, a lookup and a clock read per datagram, 160 to 230 ns, only with live conversations on). The DNS cache moved onto the same package: a cached lookup unchanged within noise (about 300 to 360 ns), storing with eviction 440 ns, 128 B and 1 allocation (was 575 ns, 160 B, 2 allocations). Batching per-datagram counts (the TCP idea) was not tried: the cost is small beside a socket write |
| `core/stats`, `core/net/udp/session`, `core/route/handler` | contention and memory of UDP counting, measured with many sessions at once (2026-10-06) | twelve goroutines each driving its own session into the same inbound, user and outbound: 25 ns a datagram uncounted, 230 ns counting straight into the shared atomics (the cores fought over three cache lines), 35 ns with each session counting for itself and adding to the shared counters every 64 datagrams, every second and when it ends. One goroutine: 112 ns uncounted, 145 counting (was 142 and 197 before batching); a reply 910 uncounted, 965 counting. Spreading each key's UDP numbers over sixteen cache-line stripes measured 76 ns but slowed one session and every read, and was reverted (owner). The price of batching: a read lags a session by up to 63 datagrams a direction and about a second. Tracking and ending a live entry with twelve goroutines: 1394 ns before, 440 ns with the registry's live list split over sixteen locks (serial 275 ns, 1 allocation). A remembered routing decision measured 233 bytes and a listed UDP destination 487 (10 000 of each; I had guessed 200 and 260), so a session at both limits holds about 0.74 MB. 200 sessions: 4 goroutines each counting the one that runs the handler, the relay's two and the reply loop; before the shared sweeper there were 2 more each. A UDP session through the handler held about 146 KB (64 KB buffers for client packets and for replies) and holds about 31 KB with 8 KB buffers (2026-10-06); with a SOCKS5 association and the NAT's buffer about 48 KB, against about 256 KB. The base UDP socket's read with the kernel's truncation flag (`ReadMsgUDPAddrPort`) measured the same as the read it replaced, about 2.3 to 4.7 µs a datagram on loopback, dominated by the system call, 0 allocations |
| `core/stats`, `core/route/handler`, `core/dns` | failure reasons, rule hits, live conversations, dial health and DNS counters (2026-10-06) | one atomic add per event (a refusal by reason 16 ns, a rule hit 23 ns, a DNS cache hit 18 ns); `TotalTraffic` sums a slice of the inbounds, 4 ns and 0 allocations (summing the map was 95 ns); a short connection through the handler allocates 53 times with no statistics, 56 counting, 58 with live conversations, and dial health adds none (time within noise, about 55 µs on a pipe); tracking and ending a live conversation 530 ns, 2 allocations, 304 B, and 2.2 µs per operation with twelve goroutines at once (one lock, still far more than connections a second), 9 ns when off; a live entry holds 348 bytes (10 000 hold 3.5 MB); listing 10 000 takes 14 ms and 10 001 allocations; a DNS cache hit 350 to 370 ns counting, an uncached lookup unchanged |
| `module/transport/base/tcp` | `net.KeepAliveConfig`: interval and count, both directions | new settings |
| `module/transport/overlay/websocket` | frame headers read with `Peek`/`Discard` instead of into local arrays, which escaped | 3 → 0 allocations per frame (2026-09-28) |
| `module/transport/overlay/websocket` | masking eight bytes at a time | 1.2 → 8.6 GB/s for 16 KiB |
| `module/transport/overlay/httpupgrade` | after the handshake, reads go straight to the stream below once its buffer is drained | 0 allocations; no measurable cost against bare TCP |
| `module/transport/overlay/tls` | browser ClientHello through uTLS (2026-09-29; a feature, not an optimisation) | full handshake over loopback, client and server together: `none` 2.2–2.6 ms, 977 allocs (resumed); `chrome` 3.4–4.5 ms, 1,277 allocs; `firefox` 3.0–4.3 ms; `randomized` 2.9–3.9 ms (3 runs of 200) |
| `module/sniffer/http` | reports the request headers too (`sniffed_request_header`, 2026-10-03; a feature) | a 7-header browser request: 0.6 → 3.0 µs, 3 → 13 allocations, once per sniffed connection; the header block is converted to a string once and sliced |

## Known opportunities (not yet done)

Candidates for future work. Each needs a benchmark before it is started.

| Where | What | Why it might matter |
| --- | --- | --- |
| `core/dns` | One mutex guards the cache; `ResolveCachedParallel` is slower per lookup than the serial benchmark. | Sharding by name would cut contention if lookups ever show up in a profile. |
| `shared/dnsstream` | Each query allocates its framed copy and each response its buffer (14 allocations a query). | A pooled frame buffer; only worth it if DNS over TCP or TLS carries heavy load. |
| `module/transport/overlay/tls` | The client clones its `tls.Config` (or uTLS's) on every dial to set the server name. | One allocation per connection; a small per-server-name cache would remove it. |
| `module/transport/overlay/tls` | With a fingerprint, the ClientHello is built from the preset on every dial, twice when `alpn` is set. | Around 300 extra allocations per handshake against Go's own; caching the built extensions is not safe as-is (key shares are per connection). |
| `module/transport/overlay/websocket` | Every frame is copied into a pooled buffer, header and payload together, so it goes out in one write. On the server, which does not mask, a vectored write could skip the copy. | A copy per write on the server's data path. |
| `module/transport/overlay/websocket` | The client draws a fresh masking key from `crypto/rand` for every frame. | Cheap on Linux with the vDSO `getrandom`; unmeasured elsewhere. |
| `config`, `cmd/apranik/cli` | `run` loads routing datasets twice: once in `config.CheckDatasets`, once to build the router. | A large `geosite.dat` is decoded twice at startup; `Check` could hand its loaded datasets to the build. |
| `module/api/clash` | `/connections` lists every live conversation and encodes it again for each client every second (a WebSocket) or on each plain request. | Listing 10 000 conversations takes about 14 ms (measured for the registry, not the API); one snapshot shared by the clients of a tick would cut it if several dashboards stay open on a busy core. |
