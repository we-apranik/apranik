# Apranik

[![CI](https://github.com/we-apranik/apranik/actions/workflows/ci.yml/badge.svg)](https://github.com/we-apranik/apranik/actions/workflows/ci.yml)
[![Licence: MPL 2.0](https://img.shields.io/badge/licence-MPL%202.0-blue.svg)](LICENSE)

**An open-source proxy core for censorship circumvention, written in Go.**

Apranik accepts traffic from a device, decides where it goes, and carries it over
the protocols and transports that still get through. Client apps and server panels
run the `apranik` binary with a JSON config file; its Go packages can also be used
as a library.

**Documentation: [we-apranik.github.io/apranik](https://we-apranik.github.io/apranik/)**

> [!NOTE]
> Apranik is young and its config format may still change.

## Why it exists

Censorship, in Iran and elsewhere, changes faster than proxy cores release. What
matters is how quickly a new circumvention technique reaches users. Apranik is
built so that a researcher can ship a technique as **one module**: a new folder
under `module/` and one line in the registry, with nothing else touched.

[![From a technique to a deployed module: a researcher finds a technique, writes one folder under module/, tests it there without core, adds one line to core/registry/default.go, names it by type in a config file that apranik check validates, and it ships in the apranik binary that clients and panels run. spec, the builder, core's inbound and outbound, other modules and the config parser stay untouched.](docs/assets/diagrams/research-to-deployment.excalidraw.svg)](docs/assets/diagrams/research-to-deployment.excalidraw.svg)

## Features

| Area | Available |
| --- | --- |
| Transports | TCP, UDP, TLS, WebSocket, HTTPUpgrade, PROXY protocol, Fragment |
| Protocols | SOCKS5, HTTP, Trojan, VLESS |
| Routing | Rules, Datasets, Block, Route |
| DNS | UDP, TCP, DoT, DoH, Cache |
| Sniffers | TLS, HTTP |

On the roadmap: REALITY, ECH, XTLS Vision, HTTP/2 and gRPC transports, QUIC and
UDP inbounds.

## Quick start

Build it (Go 1.27 or newer):

```sh
git clone https://github.com/we-apranik/apranik.git
cd apranik
go build -o apranik ./cmd/apranik
```

Save a first config as `config.json`: a local SOCKS5 and HTTP proxy that sends
everything straight out.

```json
{
  "version": 1,
  "log": {
    "level": "info",
    "outputs": [{ "type": "stderr", "format": "human" }]
  },
  "inbounds": [
    {
      "tag": "local",
      "listen": "127.0.0.1",
      "port": 1080,
      "transport": [{ "type": "tcp" }],
      "protocol": {
        "type": "mixed",
        "settings": {
          "socks5": {},
          "http": {}
        }
      }
    }
  ],
  "outbounds": [
    {
      "tag": "direct",
      "protocol": { "type": "freedom" }
    }
  ]
}
```

Check it, run it and try it:

```sh
./apranik check -config config.json
./apranik run -config config.json
curl --socks5-hostname 127.0.0.1:1080 https://example.com/
```

The [guides](https://we-apranik.github.io/apranik/guides/) set up a server and a
client.

## Contributing

Start with [CONTRIBUTING.md](CONTRIBUTING.md) and the
[first-module tutorial](https://we-apranik.github.io/apranik/contribute/first-module/).
A module is a package under `module/` that implements one `spec` kind, has tests
that run without core, and is registered with one line in
`core/registry/default.go`.

The docs site is in `docs/`, built with [VitePress](https://vitepress.dev) (Node 22
or newer):

```sh
cd docs
npm ci
npm run dev       # preview at http://localhost:5173/apranik/
npm run build     # the check CI runs
```

See [Editing the docs](https://we-apranik.github.io/apranik/contribute/docs/).

## Security

Report vulnerabilities by email, not in a public issue: see [SECURITY.md](SECURITY.md).

## Licence

[Mozilla Public License 2.0](LICENSE).
