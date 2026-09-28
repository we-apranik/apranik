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

Go 1.27 or newer, standard library only: there is nothing else to fetch.

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

Lists every transport, proxy protocol and sniffer compiled into this build, with
the kind each fills. A `"type"` in a config file must name one of them.

```text
SUBSYSTEM  NAME         KIND                        DESCRIPTION
transport  httpupgrade  overlay stream transport    HTTP/1.1 upgrade, then raw bytes, over the stream below
transport  tcp          base stream transport       TCP over IPv4/IPv6
transport  tls          overlay stream transport    TLS 1.2 and 1.3 over the stream below
transport  udp          base datagram transport     UDP over IPv4/IPv6
transport  ws           overlay stream transport    WebSocket (RFC 6455) over the stream below
proxy      http         stream proxy                HTTP CONNECT proxy, with Basic authentication
proxy      socks5       stream proxy                SOCKS5 proxy (RFC 1928), with UDP ASSOCIATE
proxy      trojan       stream proxy                Trojan proxy, CONNECT and UDP framed in the stream
proxy      trojan       datagram over stream proxy  Trojan proxy, CONNECT and UDP framed in the stream
sniffer    http         sniffer                     sniff host from an HTTP/1 request's Host header
sniffer    tls          sniffer                     sniff server name (SNI) from a TLS ClientHello
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
a checkout) and the modules compiled in.

## Exit status

| Code | Meaning |
| --- | --- |
| `0` | success, or help was printed |
| `1` | the command failed; the reason is on stderr |
| `2` | no such command |
