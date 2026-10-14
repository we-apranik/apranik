# Add a base stream transport

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
