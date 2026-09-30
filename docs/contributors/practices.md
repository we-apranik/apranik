# Practices

Rules for working in this codebase. Each has the rule, why it matters for
Apranik specifically, a real example from the repository, and a counter-example.
Counter-examples are illustrative unless they name a file.

## Layering and coupling

### A module imports only `spec`, `config/types` and `shared/`

**Why.** The project's claim is that a new technique is one folder. A module that
imports `core/` can no longer be reviewed, tested or moved on its own, and every
change to core becomes a change to it.

**Good**: `module/transport/tcp/transport.go` imports `config/types` and `spec`,
and nothing else from the project. `module/proxy/trojan` also imports
`shared/socksaddr`, the address codec it shares with SOCKS5, never
`module/proxy/socks5`.

**Bad** (illustrative):

```go
import "github.com/The-Apranik/Apranik/core/logging" // a module reaching into core
```

If a module needs something core has, the question is whether it belongs in
`spec`. Raise it rather than import around it.

### Name each module once, in the registry

**Why.** Registration that happens in `init()` is invisible in the call graph and
forces one global registry per process, which Go programs using Apranik cannot control.

**Good**: `core/registry/default.go`:

```go
moduleRegistry.Transports().RegisterBaseStream(tcp.NewBaseStreamTransportModule),
```

**Bad**:

```go
func init() { registry.Global.Register("tcp", New) } // global state, hidden ordering
```

### Proxy protocols never dial

**Why.** If a protocol opens its own connection, it can no longer run over TLS,
WebSocket or a DNS tunnel, and the stack model stops working.

**Good**: `streamOutbound.DialStream` in `core/outbound/proxy.go` dials the
server through the transport stack, then hands the stream to
`proxyClient.RequestStream`.

**Bad**:

```go
func (c *client) RequestStream(ctx context.Context, _ spec.Stream, m spec.Metadata) (spec.Stream, error) {
    conn, err := net.Dial("tcp", serverAddress) // bypasses the stack
    ...
}
```

## Connection ownership and lifetimes

### Whoever ends the work closes the stream

**Why.** A leaked socket in a long-running core becomes file-descriptor exhaustion
on a phone. Ownership has to be obvious from the code.

**Good**: `relay.Stream` in `core/relay/stream.go` defers `Close` on both streams
and closes both when the context is cancelled, because a blocked `Read` has no
other way out.

**Good**: when `Session.openOrReuseOutboundDatagram` loses a race to create an
outbound datagram, it closes the one it made before returning the winner's.

**Good**: `overlayStreamDialer.DialStream` in `core/builder/builder.go` closes
the stream it dialed when an overlay's handshake fails, because an overlay
leaves the stream below open on error by contract.

**Bad**: returning an error from a function that opened a stream without closing
it:

```go
serverStream, err := dialer.DialStream(ctx, server)
payload, err := client.RequestStream(ctx, serverStream, metadata)
if err != nil {
    return nil, err // serverStream leaks
}
```

(`streamOutbound.DialStream` closes `serverStream` on this path.)

### Silence is the only end of a UDP flow

**Why.** UDP has no close. Without timeouts, every client that ever sent one packet
keeps a socket open forever.

**Good**: `core/net/udp/session` ends a session after its idle timeout, and
`relay.Stream` closes a TCP connection silent in both directions. Activity only
stores a timestamp; the timer checks it, so senders never race the timer.

**Bad**: two timers on different layers bounding the same resource. The session
once released a freedom datagram after 1 minute, which silently overrode the
NAT's configurable 5-minute socket timeout. Each resource gets one idle bound,
configured beside it.

## Error handling

- Wrap with `%w` and say which component failed:
  `fmt.Errorf("outbound %q: %w", o.tag, err)`.
- Use the sentinels in `spec` (`ErrNoHalfClose`, `ErrMessageTooLong`,
  `ErrUnsupportedNetwork`, `ErrSniffNeedMoreData`) instead of inventing parallel
  ones, so callers can use `errors.Is` across module boundaries.
- In a per-packet loop, one bad packet must not end the session. `relay.Datagrams`
  drops a packet it cannot route or send and logs it at Debug.

**Bad**:

```go
var errTooBig = errors.New("mymodule: too big") // callers can't match spec.ErrMessageTooLong
```

## Concurrency

- Do slow work outside locks. `openOrReuseOutboundDatagram` calls
  `CreateDatagram` with the lock released, then checks again, so a slow outbound
  does not block sends to others.
- Close a `done` channel under the lock that guards the state it protects, so
  "closed" and "done" can never disagree (`session.Session.endWithReason`).
- Every test runs under `-race`. A test that only passes without it is a bug.

## Testing

- **Module tests never import core.** `module/sniffer/tls/sniffer_test.go` tests
  against real ClientHellos from `crypto/tls`, with no core at all.
- Prefer real bytes and real sockets on `127.0.0.1` over mocks of the thing being
  tested. `core/route/handler/handler_test.go` checks that two UDP sessions to one
  destination do not cross, using a real UDP echo server.
- Test what a user would notice: a startup error with the right location
  (`config/config_test.go` checks the path in each diagnostic), not internal
  call counts.

## Configuration

- **Refuse at startup, not on the first packet.** `Validate` refuses impossible
  values; `config.Check` refuses impossible combinations; listeners bind at build
  time.
- **Defaults live in the constructor**, and JSON overrides them.
- **Never default a map field**: `encoding/json` merges into an existing map, so a
  user cannot remove the default.
- **Name the location**: every diagnostic carries a path such as
  `routing.rules[0].action.settings.sniffers[1]`.

## Security-sensitive parsing

- **Bound every length before allocating.** `module/proxy/http/wire` caps a request
  head at 64 KiB and 128 headers; `core/sniff` peeks at most 16 KiB.
- **Decide early on garbage.** The HTTP sniffer refuses a stream from its first
  bytes when they cannot be a method, so a non-HTTP client is not held waiting.
- **Compare credentials in constant time** (`crypto/subtle` in
  `module/proxy/socks5/server.go` and `module/proxy/http/wire/wire.go`).
- **Keep secrets out of text.** Use `types.Secret` for passwords and keys; `%v`
  and `%q` print it redacted.

**Bad**:

```go
size := binary.BigEndian.Uint32(header)
buffer := make([]byte, size) // attacker chooses a 4 GiB allocation
```

## Naming and comments

- Long, self-describing names instead of comments: `udpSessionRoutingCache`,
  `forwardClientDatagramsToSession`.
- Keep a comment only for rationale the code cannot show, such as why an
  alternative was rejected, an RFC constraint or a security consequence.
- A function that only creates a local socket is `CreateDatagram`, never `Dial*`.
