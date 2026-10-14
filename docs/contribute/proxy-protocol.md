# Add a proxy protocol

The examples are `module/proxy/socks5` and `module/proxy/http`. A new stream
proxy touches:

| File | Change |
| --- | --- |
| `module/proxy/<name>/settings.go` (new) | settings, `Validate`, `Warnings` |
| `module/proxy/<name>/proxy.go` (new) | `Proxy` type, `Info`, `NewStreamServer`, `NewStreamClient`, `NewProxyModule` |
| `module/proxy/<name>/server.go`, `client.go` (new) | the two roles |
| `module/proxy/<name>/wire/` (new, optional) | the byte format, kept separate so it can be tested and fuzzed alone |
| `module/proxy/<name>/<name>_test.go` (new) | tests over `net.Pipe`, no core |
| `core/registry/default.go` | one line: `RegisterStream(<name>.NewProxyModule)` |

## Rules

- **Server.** `AcceptStream(ctx, underlyingStream)` reads the request and returns
  the payload stream and `spec.Metadata` with the destination, within `ctx`.
- **Client.** `RequestStream(ctx, underlyingStream, metadata)` writes the request
  onto a stream that already reaches the server, or holds it for the first write.
- **Never dial.** The stream is handed to you. A protocol that truly needs a second
  socket is a design question: raise it first.
- **Fallback.** For a request that is not your protocol, return
  `spec.ErrFallbackRequested` before writing anything; core replays it to the
  inbound's fallback.
- **Wrong credentials.** Wrap `spec.ErrAuthenticationFailed`, so core counts them
  apart from garbage.

## Users

If clients log in, implement `spec.UserManager` (`AddUser`, `RemoveUser`,
`ListUsers`) on the server; `module/proxy/trojan` is the example.

- A user is one element of your `users` list, decoded with unknown keys refused.
- Return `spec.ErrInvalidUser`, `spec.ErrUserExists` or `spec.ErrNoSuchUser`, with
  no credential in the message.
- Keep users in an immutable snapshot behind an `atomic.Pointer`, and compare in
  constant time.
- Refuse the other role's keys in `NewStreamServer` and `NewStreamClient`, so a
  stray key cannot leave an inbound open.

## UDP

A protocol with UDP also fills `spec.DatagramOverStreamProxyModule` (UDP framed in
the stream) or `spec.DatagramProxyModule`. It stays one module: `NewProxyModule`
returns the combination, such as `spec.StreamAndDatagramOverStreamProxyModule`, and
is registered once with `RegisterStreamAndDatagramOverStream`.

- Build the server once, so both kinds share one object (`sync.Once` in `trojan`).
- Implement `IsDatagramOverStream`, which only peeks, so core can choose between
  `AcceptStream` and `AcceptDatagramOverStream`.
