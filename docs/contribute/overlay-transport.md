# Add an overlay transport

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

func (s *server) WrapServerStream(ctx context.Context, underlyingStream spec.Stream, transport *spec.TransportMetadata) (spec.Stream, error) {
	// Once per connection, in that connection's goroutine, within ctx.
	// Read and answer the handshake on underlyingStream. A layer that learns
	// who the client really is replaces transport.Source (and writes no other
	// field); most layers ignore transport. Then:
	return &stream{underlyingStream: underlyingStream, ...}, nil
}
```

## Rules

- **Per role, once.** `NewStreamOverlayClient` and `NewStreamOverlayServer` build
  what every connection shares and refuse what their role lacks, such as a server
  without a certificate. `Validate` cannot know the role and must not read files.
- **Per connection, within `ctx`.** `WrapClientStream` and `WrapServerStream` end
  their handshake before `ctx` does; `streamdeadline.BindToContext` makes a
  blocking read honour it.
- **Core owns the connection.** Never dial or start goroutines; on error leave
  `underlyingStream` open. The returned stream's `Close` closes the one below.
- **Keep bytes read past the handshake** and hand them on.
- **Refuse like a web server** (`404`, `400`) if the protocol is HTTP-shaped.
- **Half-close.** Pass `CloseWrite` down, or return `spec.ErrNoHalfClose`.
- **Measure.** Anything that touches every byte is a hot path: benchmark it and
  allocate nothing per read or write.

A protocol that carries many streams over one connection (HTTP/2, gRPC) does
not fit this kind; raise it first.

Code two overlays need, such as the WebSocket upgrade handshake, moves to a
package under `shared/` once the second one exists.
