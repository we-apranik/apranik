# Configuration

Apranik reads one JSON file. This page shows its whole shape; each top-level key
has its own page.

[![What each top-level key of the config file does: a connection arrives on an inbound, routing picks an outbound by its rules, and the outbound sends it to the network. Datasets and dns are looked up along the way. log, stats and apis concern the running core.](../assets/diagrams/config-top-level-keys.excalidraw.svg)](../assets/diagrams/config-top-level-keys.excalidraw.svg)

## Structure

<div class="cfg-structure">

```json
{
  "version": 1,
  "min_core": "0.1.0",
  "strict": true,
  "log": {},
  "inbounds": [],
  "outbounds": [],
  "datasets": [],
  "dns": {},
  "routing": {},
  "stats": {},
  "apis": []
}
```

</div>

<div class="cfg-fields">

## Fields

| Key | Format |
| --- | --- |
| `log` | [Log](log.md) |
| `inbounds` | [Inbound](inbound/index.md) |
| `outbounds` | [Outbound](outbound/index.md) |
| `datasets` | [Dataset](datasets.md) |
| `dns` | [DNS](dns/index.md) |
| `routing` | [Routing](routing/index.md) |
| `stats` | [Stats](stats/index.md) |
| `apis` | [API](api/index.md) |

### `version`

<span class="cfg cfg-type">integer</span>
<span class="cfg cfg-req">Required</span>

The format version. Only `1` exists, and it may still change.

### `min_core`

<span class="cfg cfg-type">string</span>

The oldest Apranik version, as `major.minor.patch`, that can run this file. It is
checked first, so an older core asks for an upgrade instead of listing unknown keys.

### `strict`

<span class="cfg cfg-type">boolean</span>
<span class="cfg cfg-def">Default: <code>true</code></span>

Refuse unknown keys, so a misspelled key is an error. Set `false` only to share
one file with a newer core.

</div>

## Conventions

- **Defaults.** A key left out takes its default. A written `0` is refused, never
  read as "default" or "no limit".
- **Durations** are quoted Go durations: `"300ms"`, `"10s"`, `"1h30m"`.
- **Secrets** (passwords, UUIDs) never appear in logs, errors or warnings.
- **Modules.** A `type` in `transport`, `protocol`, `sniffers` or `dns.servers`
  names a module; its `settings` override only the keys they name.
- **Checking.** `apranik check -config config.json` reports every problem at once,
  each with its location, and runs nothing. Warnings, such as an inbound with no
  users on a public address, do not stop it.
