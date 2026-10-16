# Good first issues

Small, well-scoped tasks grounded in the current code. Each section can be pasted
straight into a GitHub issue. Before starting one, read [Practices](practices.md).

---

## 1. Fuzz the WebSocket and HTTPUpgrade server handshakes

**Description.** A `ws` or `httpupgrade` inbound reads an HTTP request from
anyone who connects (or from a CDN). `module/transport/overlay/websocket` fuzzes its frame
reader, but neither module fuzzes the request handling in `WrapServerStream`.
Add a fuzz target per module that feeds arbitrary bytes, as a client would send
them, to `WrapServerStream` over a loopback or in-memory stream, and checks that
it never panics, never reads past the 64 KiB handshake limit, and either
succeeds or returns an error.

**Files.** `module/transport/overlay/websocket/*_test.go`,
`module/transport/overlay/httpupgrade/*_test.go`

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

## 3. Tests for the inbound accept loop

**Description.** `core/inbound/inbound_test.go` covers dispatch, fallback and
`max_tcp_connections`; `core/builder`'s end-to-end tests cover a silent TLS client.
Still missing, with a fake `spec.StreamListener` and a fake
`spec.StreamProxyServer`:

- a failed handshake closes the stream and does not reach the handler;
- `Close` waits for live connections to finish.

**Files.** `core/inbound/inbound_test.go`

**Acceptance criteria.** The two behaviours are covered and pass under `-race`.

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

**Description.** `wire.ReadHead` and `wire.ParseHostPort` in
`module/proxy/http/wire/head.go` and `host_port.go` parse untrusted input. Add fuzz tests checking
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

---

## 11. A live API reference for the Clash API

**Description.** The Clash API is documented by hand in
[its page](../configuration/api/clash.md): tables and prose, with no schemas and no way to
try a request. Add an OpenAPI 3.1 description of it and a UI that can call a running core,
such as Swagger UI, Scalar or RapiDoc (Redoc's free version cannot send requests).

A workable shape, none of it decided:

- One hand-written YAML file in the repository as the single source: routes, parameters,
  request and response schemas, status codes, examples, and the bearer secret as the
  security scheme.
- The core serves it at `GET /openapi.json`, behind the secret, so an operator can point
  any tool (Swagger UI, Postman, Insomnia, Bruno) at their own core. Embedding a UI in the
  binary is not needed.
- The docs site renders the same file with a server address field that defaults to
  `http://127.0.0.1:9090`. Trying a request from the docs site needs the docs origin in the
  API's `allow_origins`; the page should say not to type a production secret into a hosted page.
- A test that every route the Clash module registers is in the file with the same methods,
  and the other way round, so the file cannot drift.
- WebSocket endpoints (`/traffic`, `/memory`, `/connections`, `/logs`) cannot be described
  in OpenAPI: describe the plain HTTP form where there is one and say the path also takes an
  upgrade.

**Files.** `module/api/clash/` (the file, the endpoint, the test), `docs/configuration/api/clash.md`,
`docs/.vitepress/sidebar.mts`

**Acceptance criteria.** Every documented endpoint is in the file; `npm run build` in `docs/`
passes; the drift test fails when a route is added without an entry; the page can list the
users of an inbound against a local core.

**Difficulty.** Medium
