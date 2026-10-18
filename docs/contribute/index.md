# Contribute

::: warning Read this first
Where circumvention is illegal, contributing can put you at risk. You can
contribute under a pseudonym, and the steps to do it safely are in
[Contribute safely](safety.md). Do them before your first commit.
:::

## Setup

You need Go (the version in `go.mod`, currently `go 1.27`) and nothing else.
The one third-party dependency, uTLS, is fetched by `go build`; adding another
needs a written justification in its pull request, as uTLS has.

```sh
git clone https://github.com/we-apranik/apranik.git
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

To try an overlay, the [guides](../guides/index.md) have server and client
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

See [Architecture](../concepts/architecture.md) for the full map.

[![Which kind of module to write: a technique that carries bytes is a transport, base if it opens the connection and overlay if it wraps one; one that speaks proxy semantics is a proxy protocol; one that recognises a protocol in the first bytes is a sniffer; one that carries DNS messages is a DNS server; one that lets another program read or control the core is an API. Every kind is one folder under module/ and one line in core/registry/default.go.](../assets/diagrams/module-kinds.excalidraw.svg)](../assets/diagrams/module-kinds.excalidraw.svg)

## Where to go next

- New here: [your first module](first-module.md), a sniffer from empty folder to a working rule.
- Add a module: [base stream transport](base-transport.md), [overlay transport](overlay-transport.md), [proxy protocol](proxy-protocol.md), [sniffer](sniffer.md), [DNS server](dns-server.md) or [API module](api-module.md).
- [Tests](tests.md) and [pull requests](pull-requests.md): what a change should contain, and the areas that need careful review.
- [Practices](practices.md) and [performance](performance.md): how the code is written.
- [Good first issues](good-first-issues.md).
- [Editing the docs](docs.md).
