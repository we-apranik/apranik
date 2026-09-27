# Introduction

Apranik reads one JSON config file. It describes **inbounds** (how traffic
arrives), **outbounds** (how it leaves) and the **log**. The format is version
`1`. How stable it stays over time is still an open question.

## Structure

<div class="cfg-structure" markdown>

```json
{
  "version": 1,
  "min_core": "0.1.0",
  "strict": true,
  "log": {},
  "inbounds": [],
  "outbounds": []
}
```

</div>

<div class="cfg-fields" markdown>

## Fields

| Key | Format |
| --- | --- |
| `log` | [Log](log.md) |
| `inbounds` | [Inbound](inbound/index.md) |
| `outbounds` | [Outbound](outbound/index.md) |

### `version`

<span class="cfg cfg-type">integer</span>
<span class="cfg cfg-req">Required</span>

The config file format version. Only `1` exists.

### `min_core`

<span class="cfg cfg-type">string</span>

The oldest Apranik version this config file needs, as `major.minor.patch`. It is
checked before any other field, so an older core says "this config needs a newer
Apranik" instead of complaining about fields it has never heard of.

### `strict`

<span class="cfg cfg-type">boolean</span>
<span class="cfg cfg-def">Default: <code>true</code></span>

Refuse unknown keys. A misspelled key is an error, not a silently ignored
setting. Set `false` only to share one file with a newer core.

</div>

## Checking a config file

```sh
apranik check -config config.json    # report every problem, run nothing
apranik run   -config config.json    # check, then serve until interrupted
```

`-config` also takes an `http://` or `https://` URL (plain HTTP is accepted with a
warning, since anyone on the path could alter the file). `-fetch-timeout` bounds
the download, default `30s`.

Every problem is reported at once, each with its location, instead of stopping at
the first:

```
time=… level=ERROR msg="Config error" location=inbounds[1].transport[0] problem="registry: no base stream transport registered as \"txcp\""
time=… level=ERROR msg="Config has errors" source=config.json
```

These lines use the format of the config's `stderr` log output when the config
can be read, so with [`"format": "human"`](log.md#format) they look like every
later line.

[![From config file to running components: load the file or URL, config.Parse (strict JSON, version, registry lookup, decode settings, Validate) into a blueprint, config.Check; every problem goes to Diagnostics. With errors, stop and print them all. Without, apranik check prints valid and exits, and apranik run calls builder.New, BuildOutbound, router.New, BuildInbound and Activate.](../assets/diagrams/config-pipeline.excalidraw.svg)](../assets/diagrams/config-pipeline.excalidraw.svg)

The steps split along one line, **reading** and **running**:

- `config.Parse` reports what cannot be read: bad JSON, unknown module names,
  settings that do not decode, a module's own `Validate` failing.
- `config.Check` reports what can be read but cannot run: a missing field, a
  conflicting pair, an unsafe value. It opens nothing, so `apranik check` works on
  a machine that could never run the config file.
- `core/builder` turns a clean config into running components. It binds
  listeners at build time, so a port conflict is a startup error.

## Value types

| Type | Written as |
| --- | --- |
| duration | a quoted Go duration: `"300ms"`, `"10s"`, `"5m"`, `"1h30m"`. A bare number is refused, since no unit can be assumed. Where noted, a negative value disables the limit. |
| secret | a string. Apranik never prints it in logs, errors or warnings. |
| address | an IP literal for `listen`; an IP or a domain for `server`. A domain stays unresolved until the base transport dials it. |

## Modules and their settings

Anything named by `"type"` in `transport`, `protocol` or `sniffers` is a
**module**. Its `settings` object is decoded straight into the module, and JSON
overrides only what it names; everything else keeps the module's default.

<div class="grid cards" markdown>

-   :material-swap-horizontal:{ .lg .middle } **Proxy protocols**

    ---

    [`socks5`](protocol/socks5.md) · [`http`](protocol/http.md) ·
    [`trojan`](protocol/trojan.md), plus the special protocols
    [`mixed`](inbound/mixed.md), [`freedom`](outbound/freedom.md) and
    [`block`](outbound/block.md).

-   :material-lan-connect:{ .lg .middle } **Transports**

    ---

    [`tcp`](transport/tcp.md) · [`udp`](transport/udp.md), stacked as
    described in [Transport](transport/index.md).

-   :material-magnify-scan:{ .lg .middle } **Sniffers**

    ---

    [`tls`](sniffing.md#tls) · [`http`](sniffing.md#http): recognise the
    protocol at the start of a connection.

-   :material-timer-outline:{ .lg .middle } **Timeouts**

    ---

    Every timeout, where it is set and its default, on
    [one page](timeouts.md).

</div>

## Warnings

Some findings do not stop Apranik but deserve attention, for example:

- an inbound with no username: anyone who can reach it may use it;
- `endpoint-independent` NAT filtering: an off-path sender can inject packets;
- a sniffing timeout over 5 seconds;
- no log outputs;
- more than one outbound: there are no routing rules yet, so only the first is
  used (logged by `apranik run`).

Trojan has no encryption of its own and no TLS overlay exists yet, so a Trojan
stack today sends its password hash and payload in the clear.

## Writing a config in Go

A Go program can skip JSON entirely: fill a `blueprint.Blueprint` with module
values (for example `tcp.NewBaseStreamTransportModule()`), run `config.Check` on
it, and pass it to `builder.New` and `BuildInbound` / `BuildOutbound`. See
`cmd/apranik/cli/run.go` for the complete wiring.
