# Good first issues

Small, well-scoped tasks grounded in the current code. Each section can be pasted
straight into a GitHub issue. Before starting one, read [Practices](practices.md).

---

## 1. Fuzz the WebSocket and HTTPUpgrade server handshakes

**Description.** A `ws` or `httpupgrade` inbound reads an HTTP request from
anyone who connects (or from a CDN). `module/transport/websocket` fuzzes its frame
reader, but neither module fuzzes the request handling in `WrapServerStream`.
Add a fuzz target per module that feeds arbitrary bytes, as a client would send
them, to `WrapServerStream` over a loopback or in-memory stream, and checks that
it never panics, never reads past the 64 KiB handshake limit, and either
succeeds or returns an error.

**Files.** `module/transport/websocket/*_test.go`,
`module/transport/httpupgrade/*_test.go`

**Acceptance criteria.** `go test -fuzz` runs for a minute without a failure;
seed inputs include a valid upgrade, a wrong path and an oversized header.

**Difficulty.** Easy–medium

---

## 2. Unit tests for the router

**Description.** `core/route/router` has no tests. Add tests for `Router.Route`:
it returns the default outbound, and returns an error ("router: no outbound is
configured") when constructed with a nil outbound.

**Files.** `core/route/router/router_test.go` (new)

**Acceptance criteria.** Both behaviours are covered; the tests import nothing
from `core` except the router.

**Difficulty.** Easy

---

## 3. Tests for the inbound accept loop and handshake timeout

**Description.** `core/inbound/inbound_test.go` covers dispatch, fallback and
`max_connections`; `core/builder`'s end-to-end tests cover a silent TLS client.
Still missing, with a fake `spec.StreamListener` and a fake
`spec.StreamProxyServer`:

- a proxy handshake that never completes is abandoned after `HandshakeTimeout`;
- a failed handshake closes the stream and does not reach the handler;
- `Close` waits for live connections to finish.

**Files.** `core/inbound/inbound_test.go`

**Acceptance criteria.** The three behaviours are covered and pass under `-race`.

**Difficulty.** Medium

---

## 4. Fuzz the SOCKS5 address codec

**Description.** `socksaddr.ReadAddress` and `socksaddr.ParseAddress` in
`shared/socksaddr/socksaddr.go` parse attacker-controlled bytes for both SOCKS5
and Trojan. Add a Go fuzz test that feeds arbitrary input to both and, when one
succeeds, checks that `AppendAddress` of the result reads back to the same
`spec.Destination`, and that the two parsers agree.

**Files.** `shared/socksaddr/socksaddr_test.go`

**Acceptance criteria.** `go test -fuzz=FuzzParseAddress -fuzztime=60s` finds no
panic; seed corpus covers IPv4, IPv6 and domain forms.

**Difficulty.** Easy–medium

---

## 5. Fuzz the HTTP request-head reader

**Description.** `wire.ReadHead` and `wire.ParseAddress` in
`module/proxy/http/wire/wire.go` parse untrusted input. Add fuzz tests checking
that they never panic and that `ReadHead` never returns more than its documented
head and header limits.

**Files.** `module/proxy/http/wire/wire_test.go` (new)

**Acceptance criteria.** Both fuzz targets run for 60 seconds without failure.

**Difficulty.** Easy–medium

---

## 6. Fuzz the TLS and HTTP sniffers

**Description.** `module/sniffer/tls` and `module/sniffer/http` read the first bytes
of any client. Add fuzz tests asserting that `Sniff` (on the `Sniffer` that `NewSnifferModule().NewSniffer()` builds) never panics, and that for any
input it returns either a result, `spec.ErrSniffNeedMoreData`, or another error.
Seed with a real ClientHello (the existing test helper builds one).

**Files.** `module/sniffer/tls/sniffer_test.go`, `module/sniffer/http/sniffer_test.go`

**Acceptance criteria.** Both targets run for 60 seconds without failure.

**Difficulty.** Easy

---

## 7. Test that a proxy outbound closes its stream when the handshake fails

**Description.** `streamOutbound.DialStream` closes the stream it dialed when
`RequestStream` fails. The UDP path has this test
(`TestAFailedDatagramRequestClosesTheServerStream`); the TCP path does not. Add
one with a fake dialer and a failing fake proxy client that checks the stream is
closed and the error names the outbound's tag.

**Files.** `core/outbound/outbound_test.go`

**Acceptance criteria.** The close is asserted; the test runs under `-race`.

**Difficulty.** Easy

---

## 8. Test half-close propagation in `relay.Stream`

**Description.** `relay.Stream` in `core/relay/stream.go` calls `CloseWrite` on the
far side when one direction finishes. Add a test with two fake streams that
records `CloseWrite` calls, and a second test where `CloseWrite` returns
`spec.ErrNoHalfClose` and the relay still ends cleanly.

**Files.** `core/relay/stream_test.go` (new)

**Acceptance criteria.** Both cases are covered.

**Difficulty.** Easy–medium

---

## 9. Tests for `apranik check`

**Description.** `cmd/apranik/cli` has no tests. Test `runCheck` against a valid
config file (returns nil) and one with an unknown module (returns an error), using
temporary files.

**Files.** `cmd/apranik/cli/check_test.go` (new)

**Acceptance criteria.** Both outcomes are covered without starting any listener.

**Difficulty.** Easy

---

## 10. Log the sniffed domain in the handler's relay line

**Description.** `core/inbound` logs a sniff result at Debug, and `Handler.HandleStream`
separately logs "relaying stream" with the inbound and outbound tags. Add the
sniffed protocol and domain to the handler's Debug line when present, so one line
tells the whole story.

**Files.** `core/route/handler/handler.go`

**Acceptance criteria.** Debug-level only; nothing new at Info or above.

**Difficulty.** Easy
