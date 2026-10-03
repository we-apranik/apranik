# Contributing

## Setup

You need Go (the version in `go.mod`, currently `go 1.27`) and nothing else.
The one third-party dependency, uTLS, is fetched by `go build`; adding another
needs a justification in the design notes.

```sh
git clone https://github.com/The-Apranik/Apranik.git
cd Apranik
go build ./...
go test -race ./...
```

Before sending a change, all of these must be clean:

```sh
gofmt -l .        # prints nothing
go vet ./...
go test -race ./...
```

## A local end-to-end check

The example config file opens a SOCKS5 inbound on `127.0.0.1:1080` and an HTTP
inbound on `127.0.0.1:1081`, both leaving through freedom.

```sh
go build -o apranik ./cmd/apranik
./apranik check -config cmd/apranik/config.json
./apranik run   -config cmd/apranik/config.json &

python3 -m http.server 8099 --bind 127.0.0.1 &   # a local origin
curl -x socks5h://127.0.0.1:1080 http://127.0.0.1:8099/
curl -x http://127.0.0.1:1081    http://127.0.0.1:8099/
```

To try an overlay, [Examples](configuration/examples.md) has server and client
files for Trojan over TLS, WebSocket and HTTPUpgrade; run both on one machine
with the client's `server` set to `127.0.0.1` and a self-signed certificate in
its `certificate_authorities`.

`./apranik modules` lists every module compiled into the build. Set the log
level to `debug` in a copy of the config file to see handshakes, routing and sniff
results.

## How the code is organised

- `spec/`: the contract. Changing it is a design decision; open an issue first.
- `module/`: one folder per transport, proxy protocol or sniffer.
- `shared/`: helper packages two or more modules need, one concept each.
- `core/`: everything that composes modules. Modules never import it.
- `config/`: the JSON config file.
- `cmd/apranik/`: the standalone command.

See [Architecture](architecture.md) for the full map.

## Adding a new base stream transport

The existing example is `module/transport/base/tcp`. A new base stream transport,
say `unix` for Unix domain sockets, touches these files:

| File | Change |
| --- | --- |
| `module/transport/base/unix/settings.go` (new) | `Settings` struct with JSON tags, `Validate`, `Warnings` |
| `module/transport/base/unix/transport.go` (new) | `Transport` type, `Info`, `NewStreamDialer`, `NewStreamListener`, and `NewBaseStreamTransportModule` |
| `module/transport/base/unix/unix_test.go` (new) | tests that run without core |
| `core/registry/default.go` | one line: `moduleRegistry.Transports().RegisterBaseStream(unix.NewBaseStreamTransportModule)` |

Steps:

1. **Settings.** Declare the options as a struct. Defaults are whatever your
   constructor sets; `Validate` refuses impossible values at startup; `Warnings`
   reports risky but legal ones. Use `config/types.Duration` for durations and
   `types.Secret` for anything secret.
2. **Kind.** Implement `spec.BaseStreamTransportModule`: embed the settings
   (`type Transport struct{ Settings }`), return a fixed `spec.ModuleInfo` from
   `Info` (the registry checks it never depends on a setting), and build a
   `spec.StreamDialer` and a `spec.StreamListener`.
3. **Streams.** Returned streams must satisfy `spec.Stream`. If yours cannot
   half-close, `CloseWrite` returns `spec.ErrNoHalfClose`.
4. **Resolution.** A base transport is the only layer allowed to resolve a domain
   `Destination`. Do not resolve earlier, and do not resolve anything but your own
   target.
5. **Binding.** `NewStreamListener` binds immediately, so a port conflict fails at
   startup, and it refuses a zero bind address rather than listening on every
   interface.
6. **Constructor.** Export `func NewBaseStreamTransportModule() spec.BaseStreamTransportModule`.
   The return type is the kind, so registering it as the wrong kind does not
   compile.
7. **Register.** Add the one line to `registry.Default()`. The name from `Info` is
   now valid as a `"type"` in config files.

## Adding an overlay transport

An overlay wraps one connection at a time: TLS, WebSocket, HTTPUpgrade, or a
new obfuscation. The examples are `module/transport/overlay/tls` (a handshake, then
`crypto/tls` does the rest), `module/transport/overlay/websocket` (a handshake, then
framing) and `module/transport/overlay/httpupgrade` (a handshake only). A new one,
say `obfs`, touches:

| File | Change |
| --- | --- |
| `module/transport/overlay/obfs/settings.go` (new) | `Settings`, `Validate`, `Warnings` |
| `module/transport/overlay/obfs/transport.go` (new) | `Transport{Settings}`, `Info`, `NewStreamOverlayClient`, `NewStreamOverlayServer`, `NewOverlayStreamTransportModule` |
| `module/transport/overlay/obfs/client.go`, `server.go`, `stream.go` (new) | the two roles and the stream they return |
| `module/transport/overlay/obfs/obfs_test.go` (new) | both roles over a loopback TCP pair, no core |
| `core/registry/default.go` | one line: `RegisterOverlayStream(obfs.NewOverlayStreamTransportModule)` |

The shape:

```go
func (t *Transport) NewStreamOverlayServer() (spec.StreamOverlayServer, error) {
	// Once, at startup: load keys or certificates, refuse a missing one.
	return &server{...}, nil
}

func (s *server) WrapServerStream(ctx context.Context, underlyingStream spec.Stream) (spec.Stream, error) {
	// Once per connection, in that connection's goroutine, within ctx.
	// Read and answer the handshake on underlyingStream, then:
	return &stream{underlyingStream: underlyingStream, ...}, nil
}
```

Rules:

- **Per role, once.** `NewStreamOverlayClient` and `NewStreamOverlayServer` build
  what every connection shares, and refuse a setting their role needs but
  lacks (a server without a certificate), so the mistake stops `apranik run` at
  startup. `Validate` cannot know the role and must not read files.
- **Per connection, within `ctx`.** `WrapClientStream(ctx, underlyingStream,
  server)` and `WrapServerStream(ctx, underlyingStream)` finish their handshake
  before `ctx` ends; a blocking read must fail when it does (see
  `streamdeadline.BindToContext` in `shared/streamdeadline`). The client gets
  the server's address as dialed, for an SNI or a `Host`.
- **Core owns the connection.** Never dial, never start an accept loop or a
  goroutine per connection, and on error leave `underlyingStream` open: core
  closes it. The returned stream's `Close` closes the stream below.
- **Keep bytes read past the handshake.** If you read the handshake through a
  buffer, the stream you return must hand on whatever the buffer already holds.
- **Refuse like a web server** when the handshake is wrong, if the protocol is
  HTTP-shaped: a `404` or `400`, not a silent close.
- **Half-close.** Pass `CloseWrite` down if the protocol can express it; return
  `spec.ErrNoHalfClose` if it cannot (WebSocket's Close ends both directions).
- **Measure.** Anything that touches every byte (framing, masking) is a hot
  path: benchmark it, allocate nothing per read or write.

A protocol that carries many streams over one connection (HTTP/2, gRPC) does
not fit this kind; raise it first (open question Q17).

Code two overlays need, such as the WebSocket upgrade handshake, moves to a
package under `shared/` once the second one exists
.

## Adding a new proxy protocol

The examples are `module/proxy/socks5` and `module/proxy/http`. A new stream
proxy touches:

| File | Change |
| --- | --- |
| `module/proxy/<name>/settings.go` (new) | settings, `Validate`, `Warnings` |
| `module/proxy/<name>/proxy.go` (new) | `Proxy` type, `Info`, `NewStreamServer`, `NewStreamClient`, `NewStreamProxyModule` |
| `module/proxy/<name>/server.go`, `client.go` (new) | the two roles |
| `module/proxy/<name>/wire/` (new, optional) | the byte format, kept separate so it can be tested and fuzzed alone |
| `module/proxy/<name>/<name>_test.go` (new) | tests over `net.Pipe`, no core |
| `core/registry/default.go` | one line: `RegisterStream(<name>.NewStreamProxyModule)` |

Rules:

- **Server role:** `AcceptStream(ctx, underlyingStream)` reads and strips the request
  preamble and returns the payload stream plus `spec.Metadata` with the
  destination. Respect the context deadline; the inbound uses it to bound the
  handshake.
- **Client role:** `RequestStream(ctx, underlyingStream, metadata)` writes the
  preamble onto a stream that already reaches the server. It may write nothing
  yet if the protocol puts its header on the first write.
- **Fallback.** If a request is not your protocol, return
  `spec.ErrFallbackRequested` from `Accept*` before writing anything; core
  replays every byte to the inbound's fallback.
- **Never dial.** The stream is handed to you. If your protocol truly needs a
  second socket, look at how SOCKS5 UDP ASSOCIATE receives a
  `spec.UDPAssociateSocketFunc`, and raise the design question first.
- **Other shapes.** A protocol with UDP also fills `spec.DatagramOverStreamProxyModule` or
  `spec.DatagramProxyModule`, each with its own constructor and registration line.
  A datagram-over-stream server also implements `IsDatagramOverStream`, which
  only peeks, so core can choose between `AcceptStream` and
  `AcceptDatagramOverStream`; `module/proxy/trojan` is the example.

## Adding a new sniffer

The examples are `module/sniffer/tls` and `module/sniffer/http`. The module
value implements `spec.SnifferModule` (settings, `Info`, and `NewSniffer()`,
which prepares anything needed once), and the value it builds implements
`spec.Sniffer`. `Sniff` is called from many connections at once:

- return a `spec.SniffResult` when the bytes are recognised;
- return `spec.ErrSniffNeedMoreData` while they are only a prefix of your protocol;
- return any other error as soon as they cannot be your protocol, so a connection
  is not held waiting;
- only trim what you read; do not lowercase or otherwise rewrite it.

Register it with `moduleRegistry.Sniffers().Register(<name>.NewSnifferModule)`.

## Tests

- Tests live next to the code, in `_test.go` files, mostly as external test
  packages (`package tcp_test`).
- **A module's tests never import `core/`.** That the contract alone is enough is
  the point of the design.
- Prefer real bytes: the TLS sniffer tests use ClientHellos generated by
  `crypto/tls`; proxy tests run both roles over `net.Pipe`; overlay tests run
  both roles over a loopback TCP pair, which half-closes and has deadlines.
- Generate keys and certificates in the test (`crypto/x509`), so no key material
  is ever committed.
- Parsers of untrusted bytes get a fuzz target (`go test -fuzz`), as the Trojan
  wire format and the WebSocket frame reader have.
- End-to-end tests in `core/builder` build a server and a client from config
  files, as `apranik run` does, and carry TCP and UDP through them.
- Core tests use small fakes of `spec` interfaces (see
  `core/relay/datagram_test.go`) or real UDP sockets on `127.0.0.1`.
- Everything runs under `-race`.

## What a pull request should contain

- One change with a clear purpose, `gofmt`, `go vet` and `go test -race` clean.
- For a new module: the new folder, the registry line, tests, and nothing else. If
  you had to touch `spec/`, the builder or another module, say so in the
  description; that is a finding the project wants recorded.
- Descriptive names instead of comments. Keep a comment only for a reason a reader
  cannot recover from the code.
- No new dependency without a written justification.

## Security-sensitive areas

Flag these for careful review:

- **Parsers of untrusted bytes:** `module/proxy/*/wire`, `module/sniffer/*`, the
  proxy handshakes, the WebSocket frame reader and the upgrade handshake in
  `shared/websocket/handshake`. Bound every length and allocation, and never
  trust a declared length before checking it against what arrived.
- **TLS:** certificate verification stays on by default; `insecure` is warned
  about. Never log key material or put it in an error.
- **Authentication:** credential comparison uses `crypto/subtle`; keep it
  constant-time. Secrets are `types.Secret` and must never appear in errors,
  warnings or logs.
- **Exposure:** listeners refuse a missing bind address; inbounds without a
  username produce a warning. Keep both.
- **UDP filtering:** loosening `core/net/udp/nat` filtering lets off-path senders
  inject packets.
- **Logs:** a log naming what a user reached is evidence on a seized device.
  Destinations and sniffed domains belong at Debug level only.

## Editing these docs

The site is built with MkDocs and the Material theme from `docs/` and
`mkdocs.yml`.

```sh
python3 -m venv .venv
.venv/bin/pip install -r requirements-docs.txt
.venv/bin/mkdocs serve              # live preview at http://127.0.0.1:8000
.venv/bin/mkdocs build --strict     # the check CI runs
```

Rules that keep pages rendering the same on GitHub and on the site:

- plain GitHub-flavoured Markdown, `<details>` blocks and images only;
- every diagram is an Excalidraw drawing; no Mermaid or other diagram syntax;
- callouts use GitHub alert syntax (`> [!NOTE]`, `> [!WARNING]`);
- links to other pages are relative (`[Architecture](architecture.md)`);
- one H1 per page.

### Editing an Excalidraw diagram

Every diagram in `docs/assets/diagrams/` is an `.excalidraw.svg` file: an
ordinary SVG with the Excalidraw scene embedded in it. A new diagram is made the
same way and saved next to the others.

1. Open [excalidraw.com](https://excalidraw.com) and use **Open** to load the
   `.excalidraw.svg` file.
2. Edit the drawing.
3. Export with **Export image → SVG**, keeping **Embed scene** switched on, and save
   over the same file name.

The diagrams in this repository draw a white background, boxes with a bold
first line, and the same palette: yellow for things outside Apranik, grey for
config and data, violet for core, green for modules, orange for core helpers,
blue for the contract. Keep a new diagram in that style.
4. Update the image's alt text if the drawing now says something different; it is
   what a screen reader reads instead of the picture.
