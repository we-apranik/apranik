# Add a DNS server

A DNS server module carries DNS messages to one upstream; core builds the queries,
reads the answers, picks servers and caches. The examples are `module/dns/udp`
(datagrams) and `module/dns/tcp` and `module/dns/dot` (streams, through
`shared/dnsstream`).

| Kind | Builds from |
| --- | --- |
| `spec.StreamDNSServerModule` | `NewStreamDNSServer(dialer spec.StreamDialer)` |
| `spec.DatagramDNSServerModule` | `NewDatagramDNSServer(dialer spec.DatagramDialer)` |

Both return a `spec.DNSServer`:

- `Query(ctx, queryMessage)` sends one wire-format message and returns the one
  answer. Core calls it from many goroutines at once.
- `Close()` releases the streams or sockets the server holds, never the dialer.

## Rules

- **Never dial by yourself.** The dialer reaches the server through the outbound
  the config names; the module never sees the outbound.
- **TLS is core's.** A stream module that needs TLS returns `true` from `NeedsTLS`,
  and core folds the `tls` layer into the dialer.
- **Reuse connections** as the protocol allows; only the module knows how.

Register it with `moduleRegistry.DNSServers().RegisterStream(<name>.NewStreamDNSServerModule)`
or `RegisterDatagram`.
