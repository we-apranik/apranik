# Performance

How to make Apranik faster without making it worse. Read this before changing
anything on a hot path: the relay, the handshake, sniffing, the UDP session or
NAT, a transport, or a proxy protocol's read and write loop.

The general advice comes from a full reading of the
[Go App Optimization Guide](https://goperf.dev/) (goperf.dev, read 2026-09-27),
restated for a proxy core. Where that guide is out of date, wrong for Apranik,
or unsafe for a censorship-circumvention tool, this page says so.

## The rules

1. **Measure first, and measure after.** Write a benchmark before the change,
   keep it in the repository, and quote the before and after numbers in the
   commit or review. A change with no measured gain is reverted. `b.Loop()`
   and `b.ReportAllocs()` are the defaults.
2. **Optimise per connection and per packet, not per start-up.** A proxy's cost
   is multiplied by connections per second and packets per second. Code that
   runs once at start-up is not worth tuning.
3. **Allocation is the first thing to look at.** In a proxy, garbage collection
   is the cost that grows with load. Bytes and allocations per operation
   (`B/op`, `allocs/op`) are more stable across machines than nanoseconds, so
   trust them more.
4. **Keep it boring.** Prefer the standard library's tools (`sync.Pool`,
   `io.CopyBuffer`, `context.AfterFunc`, `sync.OnceValue`) to clever ones. An
   optimisation that needs a long comment to be safe is usually not worth it.
5. **Never trade away correctness or unobservability.** A faster TLS handshake
   that makes Apranik's traffic stand out to a censor is a regression.

## Checklist for a change on a hot path

- [ ] Is there a benchmark, run before and after, with `-count` of at least 3?
- [ ] Does anything allocate per packet or per read that could be allocated
      once per connection, or pooled?
- [ ] Is every buffer handed to another goroutine either copied or clearly
      owned by the receiver? (See "Slices keep their whole array".)
- [ ] Does every read and write that can wait on a peer have a deadline or a
      context that closes the stream or datagram?
- [ ] Does every goroutine started have an end? Who closes what makes it end?
- [ ] Are pooled objects reset before reuse, and never used after `Put`?
- [ ] Is anything logged per packet or per read? Debug logs still box their
      arguments; keep them off the per-packet path, or rate-limit them.
- [ ] Were `go test -race ./...` and the benchmarks run on the final code?

## Memory and allocation

### Pool what is short-lived, per connection, and large

`sync.Pool` pays off for objects that every connection or packet needs briefly
and that cost something to allocate: copy buffers, handshake readers, scratch
buffers. It does not pay off for long-lived objects, objects shared between
goroutines, or rarely used ones.

- Store pointers in the pool (`*[]byte`, `*bufio.Reader`), so `Put` does not
  allocate.
- Reset on the way out (`Reset(nil)` for readers), so a pooled object never
  keeps a connection or its data alive.
- Do not pool a buffer that can grow without bound: one huge request would stay
  in the pool forever. Either the size is fixed, or check `cap` before `Put`.
- Pools are package-level variables. That is the standard Go idiom and does not
  break the "no globals" rule, which is about registration and configuration.

Applied in Apranik: `core/relay` pools its 32 KB copy buffers (74 KB to 8.4 KB
per short connection); `module/proxy/http/wire` pools the 8 KB head reader
(10.2 KB to 1.9 KB per accept); `core/net/udp` pools full-size datagram
buffers for NAT and session replies (512 B and one allocation per reply to
none).

### Preallocate when the size is known, not when it is guessed

`make([]T, 0, n)` and `make(map[K]V, n)` avoid repeated growth when `n` is known.
Over-allocating is worse than letting `append` grow: a large, mostly unused
backing array still costs the garbage collector. Guessing capacities "to be
safe" is a habit from languages without a garbage collector; in Go, measure.

### Slices keep their whole array

`buf[:n]` handed to another goroutine, a channel or a cache keeps the whole
buffer alive and shares its memory with the next read. Copy (`bytes.Clone`)
when data outlives the read that produced it; do not copy when it does not.
The UDP reply path avoids both the copy and the risk by handing ownership
on: each reply is read into its own pooled buffer, and the receiver releases
it (`session.Reply.Release`) when it has finished.

### Escape analysis and interfaces

- `go build -gcflags=-m ./core/relay/` prints what moves to the heap
  (`moved to heap: x`). Use it when a benchmark shows an allocation you did not
  expect.
- Converting a value to an interface usually allocates. The goperf guide says
  it always does; that is not quite true (zero-size values, small integers and
  single bytes do not), but assume it does on a hot path. `slog` arguments are
  interfaces, so a log call on the per-packet path allocates even when the
  level is disabled.
- Returning a pointer from a `New*` constructor is idiomatic; do not contort
  code to avoid an escape that happens once per connection.

### Struct layout

Order fields largest to smallest to reduce padding; group fields of the same
size. This matters only for types there are very many of (one per packet, one
per mapping). Separate fields written by different goroutines onto different
cache lines only when a benchmark shows false sharing. The `fieldalignment`
analyser finds padding.

## Concurrency

### Goroutines are cheap, not free

Each costs a stack of at least a few KB plus scheduling. Per connection, count
them and remove the ones that only wait:

- `context.AfterFunc(ctx, closeFunc)` replaces a goroutine that waits on
  `ctx.Done()` to close something. It starts a goroutine only if the context is
  actually cancelled.
- A caller that would only wait for two workers can do one of the two jobs
  itself.

Applied in Apranik: `relay.Stream` went from 4 goroutines per TCP connection
to 2.

### Every goroutine needs an end

A goroutine blocked in `Read` on a silent peer lives forever unless something
closes the stream or a deadline fires. Apranik's rules: the handshake has a
timeout, a relayed stream has an idle timeout, a UDP session and every NAT
socket end on silence, and cancelling the context closes both streams. A new
loop must say which of these ends it.

### Atomics, mutexes and immutable data

- `sync/atomic` types for counters and flags; a mutex for anything with more
  than one field or an invariant.
- For data read on every connection and changed rarely (a future routing
  table, a reloaded config), build a new immutable value and publish it with
  `atomic.Pointer[T]`. Readers take no lock. Deep-copy maps and slices before
  publishing.
- `sync.OnceValue` / `sync.OnceValues` for lazy set-up. Hand-written atomic
  initialisation is error-prone (the goperf author's own first version had a
  race); do not write it without a benchmark showing `sync.Once` is the
  bottleneck.

### Bounded work

Unbounded input (connections, packets, queued work) needs a bound somewhere: a
worker count, a buffered channel, or a semaphore. When the bound is reached,
reject early and cheaply rather than queue. A full channel with a non-blocking
send is the simplest form (passive load shedding).

## I/O

### Copy loops

`io.CopyBuffer` with a pooled buffer, never `io.Copy` on a hot path, unless one
side is a type the kernel can copy between directly (then the buffer is simply
not used). Apranik's stream wrappers hide those types, so in practice the
buffer is always used.

### Peek, do not re-read

Read what has already arrived in one call; do not grow a peek by one byte at a
time. `peekable.Stream` reads ahead at least 4 KB, and `sniff.Recognise` shows
the sniffers every buffered byte. Before that change, a ClientHello arriving in
pieces cost the full 300 ms sniffing timeout.

`PeekExactly` exists for the opposite case: when the stream underneath is
handed on after peeking, nothing may be read ahead.

### Buffer writes only where latency allows

The goperf guide shows large throughput gains from buffering writes. For a
relay, buffering adds latency and breaks interactive protocols, so relayed
bytes are written as they arrive. Buffering is right for a handshake built from
several pieces: build the whole message, then write once (`writeStatus` in the
HTTP module does this).

## Networking

### Deadlines and keep-alives

- Every read or write that waits on a peer is bounded, by a deadline or by a
  context that closes the stream or datagram.
- TCP keep-alive in Go: since Go 1.23, `net.KeepAliveConfig` sets idle time,
  probe interval and probe count. The goperf guide predates this and says only
  the idle time can be set without raw system calls. The `tcp` module uses
  it on both dialed and accepted connections (`keep_alive`,
  `keep_alive_interval`, `keep_alive_count`).

### Socket options

`TCP_NODELAY` is on by default in Go and in the `tcp` module; keep it on for
interactive traffic. Receive and send buffer sizes, `SO_REUSEPORT` and the
listen backlog are tuned per deployment, under load, one at a time; never copy
values from elsewhere. Options that must be set before bind go in
`net.ListenConfig.Control`.

### DNS

Go has no DNS cache, so Apranik has its own: `core/dns` caches answers for
their TTL, and every base transport resolves through it. A cache
must respect how often records change; a stale answer breaks connections, so
serving stale answers is off by default and bounded by `max_stale_age`.

Measured 2026-10-05 (`go test -bench . -benchmem ./core/dns/`, 12 threads,
`-count 3`, medians):

| Benchmark | Before | After |
| --- | --- | --- |
| `ResolveCachedOneRecordType` (a cache hit) | 626 ns, 458 B, 7 allocs | 208 ns, 24 B, 1 alloc |
| `ResolveCachedTwoRecordTypes` | 874 ns, 572 B, 9 allocs | 376 ns, 48 B, 1 alloc |
| `ResolveCachedParallel` | 810 ns, 458 B, 7 allocs | 516 ns, 24 B, 1 alloc |
| `ResolveUncached` (in-memory server) | 15.5 µs, 5.1 KB, 36 allocs | 13.3 µs, 4.5 KB, 29 allocs |

"Before" is the first working version, "after" the one committed: the
qualified name is built only for a query, the label check does not split the
name, record-type lists are shared, results stay in arrays unless two queries
run at once, and a hostname is not handed to `netip.ParseAddr` to fail. The one
allocation left is the slice returned, which the caller owns. The `udp`
transport remembers a domain's address for 5 seconds, so a stream of datagrams
to one domain does not ask the resolver per datagram: its write to a domain
stays at 0 allocations.

### TLS

Parts of the goperf TLS page are wrong for Apranik and must not be followed:

- **Fingerprint over speed.** Choosing cipher suites, curves or extension
  order changes the ClientHello fingerprint. A censor can block an unusual
  fingerprint. Any TLS tuning must be judged first by how it looks on the wire.
- **No certificate-verification cache that skips checks.** The guide's example
  returns success for any previously seen leaf certificate, without checking
  the host name or expiry. That is a security hole.
- **Out of date.** `PreferServerCipherSuites` is ignored by current Go,
  `CipherSuites` does not apply to TLS 1.3, and session ticket keys rotate
  automatically when left unset (`SetSessionTicketKeys` is the way to share
  them between instances). Go's defaults now include a
  post-quantum key exchange, which the guide's curve list would disable.
- **What does apply:** session resumption and ALPN matter for handshake
  latency, and Go's defaults are already fast (the goperf tracking reports
  about 58% faster TLS handshakes in Go 1.25 than in 1.23).

Applied in `module/transport/overlay/tls`: the client sends a browser's ClientHello
through uTLS (Chrome by default), whose cipher suites, curves and extensions are
the preset's and are never tuned; the server loads its certificates once, at
startup, not per connection. Each outbound shares one resumption cache across
its connections, but uTLS's browser presets carry no `pre_shared_key`
extension, so TLS 1.3 sessions resume only with `"fingerprint": "none"`. A
fingerprinted handshake costs about 1.5 times Go's own in
`BenchmarkClientHandshake` (Go's resumes there, which accounts for part of it).

### QUIC

QUIC is an Open design question in Apranik. The goperf page is an introduction
to `quic-go`, whose adoption would be the project's first dependency; nothing
there changes that decision.

## Garbage collector and runtime settings

`GOGC`, `GOMEMLIMIT`, `GOMAXPROCS`, thread pinning and CPU affinity are the
deployer's decisions, not the code's. Apranik does not set them. The advice
worth passing on to deployers: in a container, set `GOMEMLIMIT` somewhat below
the container's memory limit, so the collector works harder before the process
is killed. Change `GOGC` only with a profile showing the collector is the
bottleneck.

Two runtime debugging variables in the goperf networking pages
(`netpollWaitLatency`, `netpoll=1`) do not appear in Go's documentation; do
not rely on them.

## Profiling

- Micro-benchmarks: see "Benchmarks and CI" below; compare runs with
  `benchstat`. Treat differences under about 5% as noise
  unless allocation counts also change, which are deterministic.
- CPU and heap profiles under load: `go test -cpuprofile` / `-memprofile` on a
  benchmark, then `go tool pprof -http=:7070`. Look for `runtime.mallocgc`,
  `runtime.gcBgMarkWorker`, `runtime.convT` (interface boxing) and scheduler
  functions (`runtime.schedule`, `findRunnable`).
- Goroutine counts: `runtime.NumGoroutine()` before and after starting N
  connections, divided by N.
- Loopback benchmarks measure Apranik's code, not the network. Use them to
  compare versions of Apranik, never to claim real-world throughput.

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

## Benchmarks and CI

Every hot path has a benchmark in the package it measures:

| Path | Benchmark |
| --- | --- |
| TCP relay | `core/relay`: `BenchmarkStreamRelayOneShortExchange` |
| Sniffing | `core/sniff`: `BenchmarkSniffTLSClientHello` |
| Outbound group | `core/outbound/group`: `BenchmarkGroupDialStream` |
| Routing | `core/route/router`: `BenchmarkRouteRealisticConfig`; `core/route/informant`: `BenchmarkRouteStreamWithoutRules` |
| Matchers and datasets | `core/route/router/match`: domain, prefix and `ip_is_private` lookups; `core/route/dataset/geodata`: `BenchmarkReadOneCategoryOfMany` |
| HTTP handshake | `module/proxy/http`: `BenchmarkAcceptConnect` |
| UDP socket | `module/transport/base/udp`: `BenchmarkSocketWriteToIP`, `…CachedDomain`, `BenchmarkSocketReadFrom` |
| SOCKS5 UDP | `module/proxy/socks5`: `BenchmarkAssociationReadFrom`, `…WriteTo` |
| Trojan UDP | `module/proxy/trojan`: `BenchmarkStreamDatagramReadFrom`, `…WriteTo` |
| Address codec | `shared/socksaddr`: `BenchmarkReadAddressIPv4`, `BenchmarkParseAddressIPv4` |
| UDP session | `core/net/udp/session`: `BenchmarkSessionSend`, `BenchmarkSessionReply` |
| NAT | `core/net/udp/nat`: `BenchmarkNATWriteTo`, `BenchmarkNATReadFrom` |
| Peer address | `spec`: `BenchmarkNewEndpointFromTCPAddr` |
| WebSocket framing | `module/transport/overlay/websocket`: `BenchmarkClientToServer1KiB`, `…16KiB`, `…64KiB`, `BenchmarkServerToClient16KiB`, `BenchmarkMask16KiB` |
| HTTPUpgrade | `module/transport/overlay/httpupgrade`: `BenchmarkClientToServer16KiB` against `BenchmarkBareTCP16KiB` |
| TLS handshake | `module/transport/overlay/tls`: `BenchmarkClientHandshake/none`, `…/chrome`, `…/firefox`, `…/randomized` |

The UDP ones use stand-ins below the layer they measure, so they report that
layer's cost alone. A new hot path gets a benchmark in the same change.

Locally, before and after a change:

```sh
go test -run '^$' -bench . -benchmem -count 6 ./... > before.txt
# make the change
go test -run '^$' -bench . -benchmem -count 6 ./... > after.txt
benchstat before.txt after.txt   # go install golang.org/x/perf/cmd/benchstat@latest
```

On every pull request, `.github/workflows/benchmarks.yml` runs the same
comparison against the base and puts the table in the run summary. It never
fails a build: shared runners are noisy, so read `B/op` and `allocs/op` first.

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
| `core/stats`, `core/net/udp/session`, `core/relay`, `core/route/handler` | UDP counting (a design note, 2026-10-06) | `Session.Send` 142 ns without counting, 197 ns counting under three counter sets (inbound, user, outbound: two atomics each, bytes and packets), 0 allocations; a reply through the session 1185 ns without, 1280 ns counting; four counter sets cost 95 ns per datagram in isolation; a routing-cache hit (read lock, a clock read) 100 ns serial, 293 ns with twelve goroutines on one destination, 0 allocations; `TotalTraffic` now reads four values per inbound (about 30 ns for two inbounds, 0 allocations; it was 4 ns when it read two); a live entry after the smaller counters and sequence ID: 254 bytes (was 348), tracking and ending one 284 ns and 1 allocation (was 530 ns and 2), 1.4 µs per operation with twelve goroutines (was 2.2 µs), listing 10 000 takes 5.1 ms (was 14 ms). After the route cache became a bounded `shared/lru` (2026-10-06): a cache hit 128 ns serial (was 100: a hit now moves the entry to the front under one lock), about 500 ns with twelve goroutines hammering one destination of one session (was 293; a real session has two goroutines), 0 allocations; adding a route at the limit 215 ns and one allocation; the idle sweep stops at the first route that is not idle, so it costs what it removes. UDP live entries are their own bounded `shared/lru` per session (a lock, a lookup and a clock read per datagram, 160 to 230 ns, only with live conversations on). The DNS cache moved onto the same package: a cached lookup unchanged within noise (about 300 to 360 ns), storing with eviction 440 ns, 128 B and 1 allocation (was 575 ns, 160 B, 2 allocations). Batching per-datagram counts (the TCP idea) was not tried: the cost is small beside a socket write |
| `core/stats`, `core/net/udp/session`, `core/route/handler` | contention and memory of UDP counting, measured with many sessions at once (2026-10-06) | twelve goroutines each driving its own session into the same inbound, user and outbound: 25 ns a datagram uncounted, 230 ns counting straight into the shared atomics (the cores fought over three cache lines), 35 ns with each session counting for itself and adding to the shared counters every 64 datagrams, every second and when it ends. One goroutine: 112 ns uncounted, 145 counting (was 142 and 197 before batching); a reply 910 uncounted, 965 counting. Spreading each key's UDP numbers over sixteen cache-line stripes measured 76 ns but slowed one session and every read, and was reverted (owner). The price of batching: a read lags a session by up to 63 datagrams a direction and about a second. Tracking and ending a live entry with twelve goroutines: 1394 ns before, 440 ns with the registry's live list split over sixteen locks (serial 275 ns, 1 allocation). A remembered routing decision measured 233 bytes and a listed UDP destination 487 (10 000 of each; I had guessed 200 and 260), so a session at both limits holds about 0.74 MB. 200 sessions: 4 goroutines each counting the one that runs the handler, the relay's two and the reply loop; before the shared sweeper there were 2 more each. A UDP session through the handler held about 146 KB (64 KB buffers for client packets and for replies) and holds about 31 KB with 8 KB buffers (a design note, 2026-10-06); with a SOCKS5 association and the NAT's buffer about 48 KB, against about 256 KB. The base UDP socket's read with the kernel's truncation flag (`ReadMsgUDPAddrPort`) measured the same as the read it replaced, about 2.3 to 4.7 µs a datagram on loopback, dominated by the system call, 0 allocations |
| `core/stats`, `core/route/handler`, `core/dns` | failure reasons, rule hits, live conversations, dial health and DNS counters (a design note, 2026-10-06) | one atomic add per event (a refusal by reason 16 ns, a rule hit 23 ns, a DNS cache hit 18 ns); `TotalTraffic` sums a slice of the inbounds, 4 ns and 0 allocations (summing the map was 95 ns); a short connection through the handler allocates 53 times with no statistics, 56 counting, 58 with live conversations, and dial health adds none (time within noise, about 55 µs on a pipe); tracking and ending a live conversation 530 ns, 2 allocations, 304 B, and 2.2 µs per operation with twelve goroutines at once (one lock, still far more than connections a second), 9 ns when off; a live entry holds 348 bytes (10 000 hold 3.5 MB); listing 10 000 takes 14 ms and 10 001 allocations; a DNS cache hit 350 to 370 ns counting, an uncached lookup unchanged |
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
