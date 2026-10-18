---
description: "What a pull request to Apranik should contain: one change, clean checks, and a new module touching nothing else."
---

# Pull requests

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
- **Exposure:** listeners refuse a missing bind address; inbounds whose users are
  none (SOCKS5, HTTP) produce a warning. Keep both.
- **UDP filtering:** loosening `core/net/udp/nat` filtering lets off-path senders
  inject packets.
- **Logs:** a log naming what a user reached is evidence on a seized device.
  Destinations and sniffed domains belong at Debug level only.
