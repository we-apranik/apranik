# Install and run

There is no release yet, so you build `apranik` from source. You need Go 1.27 or
newer; `go build` fetches the two dependencies.

```sh
git clone https://github.com/we-apranik/apranik.git
cd apranik
go build -o apranik ./cmd/apranik
```

Check the build:

```sh
./apranik version
```

The output lists the version, the Go version, the platform and every module built
in: transports, proxy protocols, sniffers, DNS servers and APIs. `apranik modules`
lists them with a one-line description of each.

The binary reads one JSON config file. The next page writes a first one.
`apranik help` lists every command.
