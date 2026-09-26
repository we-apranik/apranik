# Configuration

Apranik reads one JSON config file describing its inbounds (how traffic arrives) and
outbounds (how it leaves). The format is implemented as version `1`. How stable it
stays over time is still an open question.

## From config file to running components

[![From config file to running components: load the file or URL, config.Parse (strict JSON, version, registry lookup, decode settings, Validate) into a blueprint, config.Check; every problem goes to Diagnostics. With errors, stop and print them all. Without, apranik check prints valid and exits, and apranik run calls builder.New, BuildOutbound, router.New, BuildInbound and Activate.](assets/diagrams/config-pipeline.excalidraw.svg)](assets/diagrams/config-pipeline.excalidraw.svg)

The steps split along one line: **reading** and **running**.

- `config.Parse` (in `config/build.go`) reports what cannot be read: bad JSON,
  unknown module names, settings that do not decode, a module's own `Validate`
  failing. It looks each `type` up in the registry by position: index 0 of a
  transport stack must be a base transport, later entries must be overlays.
- `config.Check` (in `config/check.go`) reports what can be read but cannot run:
  a missing field, a conflicting pair, an unsafe value. It takes no registry and
  opens nothing, so `apranik check` works on a machine that could never run the
  config file, and it applies equally to a blueprint written directly in Go.
- `core/builder` turns a clean blueprint into running components. It binds
  listeners at build time, so a port conflict is a startup error.

Every problem is reported at once, each with a location such as
`inbounds[0].protocol.settings`, rather than stopping at the first.

## Config file shape

[![Shape of a config file: top level version, min_core and strict; log; inbounds, each with tag, listen, port, network, transport, protocol, optional sniffing and timeouts; outbounds, each one of a proxy protocol (server, transport, protocol, timeouts), freedom (settings tcp, udp, nat) or block (nothing else).](assets/diagrams/config-shape.excalidraw.svg)](assets/diagrams/config-shape.excalidraw.svg)

```json
{
  "version": 1,
  "min_core": "0.1.0",
  "log": { "level": "info", "outputs": [{ "type": "stderr" }] },

  "inbounds": [
    {
      "tag": "socks-in",
      "listen": "127.0.0.1",
      "port": 1080,
      "network": "tcp",
      "handshake_timeout": "10s",
      "transport": [{ "type": "tcp" }],
      "sniffing": { "sniffers": [{ "type": "tls" }, { "type": "http" }] },
      "protocol": { "type": "socks5", "settings": { "udp_relay": true } }
    }
  ],

  "outbounds": [
    { "tag": "direct", "protocol": { "type": "freedom" } },
    { "tag": "drop", "protocol": { "type": "block" } },
    {
      "tag": "upstream",
      "server": "203.0.113.5",
      "server_port": 1080,
      "transport": [{ "type": "tcp" }],
      "protocol": { "type": "socks5" }
    }
  ]
}
```

### Top level

| Key | Meaning |
| --- | --- |
| `version` | config file format version; required; currently `1` |
| `min_core` | oldest Apranik version the config file needs; checked before any other field |
| `strict` | refuse unknown keys; default `true` |
| `log` | `level` (one of the eight RFC 5424 severities) and `outputs` (`stderr`, `file`, `memory`, `syslog`); an empty list is silent, with a warning |

### Inbound

| Key | Meaning |
| --- | --- |
| `tag` | unique name, used in logs |
| `listen`, `port` | address and port; required, with no default, so nothing is exposed on every interface by omission |
| `network` | `tcp` (default) or `udp`; `udp` is accepted by the parser but refused by the builder, because no datagram proxy module exists yet |
| `handshake_timeout` | bound on the proxy handshake; default `10s`, negative disables |
| `tcp_idle_timeout` | closes a TCP connection silent in both directions; default `5m`, negative disables |
| `udp_session_idle_timeout` | ends a silent UDP session; default `5m`; must not be negative |
| `transport` | the stack, base first |
| `protocol` | the proxy protocol, `{ "type": ..., "settings": {...} }` |
| `sniffing` | absent means off; `sniffers` (at least one) and `timeout` (default `300ms`); TCP only |
| `fallback` | `{ "server": ..., "server_port": ... }`: where a request the proxy refuses is replayed, over plain TCP, from its first byte; absent means such a request is closed. TCP only. Today only `trojan` refuses into it |

### Outbound

| Key | Meaning |
| --- | --- |
| `tag` | unique name |
| `protocol` | a proxy protocol, or one of the special protocols `freedom` and `block` |
| `server`, `server_port` | the proxy server; proxy protocols only |
| `transport` | the stack used to reach the server; proxy protocols only |
| `dial_timeout` | bound on reaching the server (or, for freedom, the destination); default `10s`, negative disables |
| `handshake_timeout` | bound on the proxy handshake with the server; proxy protocols only; default `10s`, negative disables |

Inbounds and outbounds share one shape: a `transport` stack and a `protocol`.
The two special outbound protocols take no server and no transport:

- `freedom` sends straight to each destination over plain TCP and UDP. Its
  `settings` may hold `tcp` and `udp` (those modules' settings) and `nat`
  (`mapping`, `filtering`, `idle_timeout`, in RFC 4787 terms).
- `block` refuses every connection and drops every UDP packet. It takes no
  settings and no `dial_timeout`.

[![RFC 4787 in freedom's NAT. Mapping: endpoint-independent (default) uses one socket for every destination, address-dependent one per destination IP, address-and-port-dependent one per IP and port. Filtering on a socket that sent to 1.1.1.1:53: endpoint-independent accepts anyone, address-dependent accepts any port of 1.1.1.1, address-and-port-dependent (default) accepts only 1.1.1.1:53.](assets/diagrams/nat-behaviours.excalidraw.svg)](assets/diagrams/nat-behaviours.excalidraw.svg)

### Trojan with a fallback

```json
{
  "tag": "trojan-in",
  "listen": "0.0.0.0",
  "port": 443,
  "transport": [{ "type": "tcp" }],
  "protocol": {
    "type": "trojan",
    "settings": { "users": [{ "name": "alice", "password": "correct horse" }] }
  },
  "fallback": { "server": "127.0.0.1", "server_port": 8080 }
}
```

A Trojan outbound carries both TCP and UDP; its UDP travels framed inside the
stream to the server.

## Module settings and defaults

A module's `settings` object is decoded straight into the module value that its
constructor returned. The constructor's field values are the defaults, and JSON
overrides only what it names. For example, `tcp.NewBaseStreamTransportModule`
starts with `keep_alive` 15s and `no_delay` true.

| Module | Settings |
| --- | --- |
| `tcp` | `keep_alive` (duration, default `15s`), `no_delay` (default `true`) |
| `udp` | `resolve_timeout` (default `5s`), `resolve_cache_ttl` (default `30s`) |
| `socks5` | `username`, `password`, `udp_relay` (default `true`) |
| `http` | `username`, `password` |
| `trojan` | inbound: `users`, a list of `{ "name", "password" }`, names and passwords unique, the matched name reported as the user; outbound: `password` |
| sniffers `tls`, `http` | none yet |

Credentials use `types.Secret`, which never prints its value.

> [!WARNING]
> Two `encoding/json` traps apply to every settings type. A map field given a
> default keeps it after decoding (maps merge, slices replace), so never give a
> map a default. And an embedded type with its own `UnmarshalJSON` takes over the
> whole outer decode, silently dropping every other field.

## Timeouts

Every timeout is configured beside the component it bounds; there is no central
timeouts section.

[![Every timeout: for a TCP connection in order, inbound handshake_timeout 10s, sniffing timeout 300ms, outbound dial_timeout 10s, outbound handshake_timeout 10s, then relay until tcp_idle_timeout 5m of silence; TCP keep-alive 15s and the -fetch-timeout flag. For a UDP session: udp_session_idle_timeout 5m, NAT idle_timeout 5m, resolver 5s with a 30s cache, and the internal 1m cached route.](assets/diagrams/timeouts.excalidraw.svg)](assets/diagrams/timeouts.excalidraw.svg)

| Timeout | Where | Default |
| --- | --- | --- |
| Client handshake | `inbounds[].handshake_timeout` | `10s` |
| Sniffing wait | `inbounds[].sniffing.timeout` | `300ms` |
| TCP idle | `inbounds[].tcp_idle_timeout` | `5m` |
| UDP session idle | `inbounds[].udp_session_idle_timeout` | `5m` |
| Dial | `outbounds[].dial_timeout` | `10s` |
| Server handshake | `outbounds[].handshake_timeout` | `10s` |
| Trojan request wait for the first payload | internal | `100ms` |
| Fallback dial | internal | `10s` |
| NAT socket idle | freedom `settings.nat.idle_timeout` | `5m` |
| TCP keep-alive | `tcp` settings `keep_alive` (for freedom: `settings.tcp.keep_alive`) | `15s` |
| UDP domain lookup | freedom `settings.udp.resolve_timeout` | `5s` |
| UDP lookup cache | freedom `settings.udp.resolve_cache_ttl` | `30s` |
| Remote config file download | `apranik run/check -fetch-timeout` | `30s` |

Two stay internal: a cached UDP route is forgotten after 1 minute of silence, and
the accept loop pauses 10ms after a failed accept.

## Warnings worth knowing

`config.Check` and each module's `Warnings` produce non-fatal findings, for
example:

- an inbound proxy with no username: anyone who can reach it may use it;
- `endpoint-independent` UDP filtering: lets an off-path sender inject packets;
- a sniffing timeout over 5 seconds;
- note that Trojan has no encryption of its own and no TLS overlay exists yet, so
  a Trojan stack today sends its password hash and payload in the clear;
- no log outputs;
- only the first outbound is used, because there are no routing rules yet
  (logged by `apranik run`).

## Writing a blueprint in Go

A Go program can skip JSON entirely: fill a `blueprint.Blueprint` with module values
(for example `tcp.NewBaseStreamTransportModule()`), run `config.Check` on it, and
pass it to `builder.New` and `BuildInbound` / `BuildOutbound`. See
`cmd/apranik/cli/run.go` for the complete wiring.
