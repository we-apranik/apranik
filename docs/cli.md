# Command line

The `apranik` binary is what client apps and server panels run. It has six
commands; `apranik help <command>` prints each one's detail.

```text
apranik run [-config source] [-fetch-timeout duration]
apranik check [-config source] [-fetch-timeout duration]
apranik modules
apranik generate <what>
apranik version
apranik help [command]
```

With no command, or with `-h`, it prints the list of commands.

## Building

```sh
go build -o apranik ./cmd/apranik
```

Go 1.27 or newer. The one dependency, uTLS, is fetched by `go build`.

## `run`

Loads the config file, checks it, builds every inbound and outbound, and serves
until `SIGINT` or `SIGTERM`.

| Flag | Default | Meaning |
| --- | --- | --- |
| `-config` | `config.json` | a file path, or an `http://` or `https://` URL |
| `-fetch-timeout` | `30s` | how long to wait for a config file given as a URL |

Every problem in the config file is reported at once, with its location, before
anything opens. A problem only found while building, such as a port already in
use or a TLS certificate file that cannot be read, stops startup with a single
error. Until the config file's own log outputs are set up, messages go to stderr
in the format that config file asked for.

## `check`

The same loading and checking as `run`, then it exits. Nothing is built and no
socket is opened, so a config file can be checked on a machine that could never
run it.

`check` does not read files a module loads while building, such as TLS
certificates: those are read by `run` at startup.

## `modules`

Lists every transport, proxy protocol and sniffer compiled into this build,
grouped by the kind each fills. A `"type"` in a config file must name one of
them. A module that fills several kinds, such as `trojan`, appears under each.

```text
Base stream transports
  tcp          TCP over IPv4/IPv6

Overlay stream transports
  fragment     cut the TLS ClientHello into records and packets to hide the server name
  httpupgrade  HTTP/1.1 upgrade, then raw bytes, over the stream below
  tls          TLS 1.2 and 1.3 over the stream below, with a browser's ClientHello
  ws           WebSocket (RFC 6455) over the stream below

Base datagram transports
  udp          UDP over IPv4/IPv6

Stream proxies
  http         HTTP CONNECT proxy, with Basic authentication
  socks5       SOCKS5 proxy (RFC 1928), with UDP ASSOCIATE
  trojan       Trojan proxy, CONNECT and UDP framed in the stream

Datagram over stream proxies
  trojan       Trojan proxy, CONNECT and UDP framed in the stream

Sniffers
  http         sniff host from an HTTP/1 request's Host header
  tls          sniff server name (SNI) from a TLS ClientHello
```

## `generate`

Generates a value a config file needs.

| What | Output |
| --- | --- |
| `uuid` | a random RFC 4122 version 4 UUID |

More kinds arrive with the modules that need them. TLS certificates are not
generated here; use your usual tooling (for example `openssl`, or a certificate
authority such as Let's Encrypt).

## `version`

Prints the version, the Go toolchain, the platform, the commit (when built from
a checkout) and the modules compiled in, grouped by kind as `modules` groups
them.

```text
apranik dev
  go        go1.27.1
  platform  linux/amd64

  Base stream transports        tcp
  Overlay stream transports     fragment, httpupgrade, tls, ws
  Base datagram transports      udp
  Stream proxies                http, socks5, trojan
  Datagram over stream proxies  trojan
  Sniffers                      http, tls
```

## Exit status

| Code | Meaning |
| --- | --- |
| `0` | success, or help was printed |
| `1` | the command failed; the reason is on stderr |
| `2` | no such command |
