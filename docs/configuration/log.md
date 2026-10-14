# Log

Where log records go, and how much is written.

## Structure

<div class="cfg-structure" markdown>

```json
{
  "log": {
    "level": "info",
    "outputs": [
      { "type": "stderr", "format": "human", "color": true },
      { "type": "file", "path": "apranik.log" }
    ]
  }
}
```

</div>

> [!NOTE]
> With no outputs, Apranik logs nothing, on purpose: a log naming what a user
> reached is evidence on a seized device.

<div class="cfg-fields" markdown>

## Fields

### `level`

<span class="cfg cfg-type">string</span>
<span class="cfg cfg-def">Default: <code>info</code></span>

The least severe level written, from most to least verbose: `debug`, `info`,
`notice`, `warning`, `error`, `critical`, `alert`, `emergency` (RFC 5424).

### `outputs`

<span class="cfg cfg-type">list of outputs</span>
<span class="cfg cfg-def">Default: <code>[]</code>, silent</span>

Where records go. Every record goes to every output. Each output's `type` is one
of the [output types](#output-types).

</div>

## Output types

=== "stderr"

    ```json
    {
      "type": "stderr",
      "format": "human",
      "color": true
    }
    ```

    <div class="cfg-fields" markdown>

    ### `format` { #format }

    <span class="cfg cfg-type">string</span>
    <span class="cfg cfg-def">Default: <code>logfmt</code></span>

    | Value | Writes |
    | --- | --- |
    | `logfmt` | `time=… level=INFO msg="Inbound active" tag=socks-in` |
    | `json` | `{"time":"…","level":"INFO","msg":"Inbound active","tag":"socks-in"}` |
    | `human` | `2026-09-27T05:24:55.324+03:30 INFO      Inbound active tag=socks-in` |

    Use `human` when a person reads the terminal.

    ### `color`

    <span class="cfg cfg-type">boolean</span>
    <span class="cfg cfg-def">Default: <code>false</code></span>
    <span class="cfg cfg-note">human only</span>

    Color the level name and dim the time. `NO_COLOR` in the environment turns it
    off.

    </div>

=== "file"

    ```json
    {
      "type": "file",
      "path": "/var/log/apranik.log",
      "format": "json"
    }
    ```

    <div class="cfg-fields" markdown>

    ### `path`

    <span class="cfg cfg-type">string</span>
    <span class="cfg cfg-req">Required</span>

    The file to append to. It is created with mode `0600` if missing.

    ### `format` { #file-format }

    <span class="cfg cfg-type">string</span>
    <span class="cfg cfg-def">Default: <code>json</code></span>

    `json`, `logfmt` or `human`, as for [stderr](#format). A file is never
    colored.

    </div>

    > [!WARNING]
    > A log file outlives the process and is evidence on a seized device.

=== "memory"

    ```json
    {
      "type": "memory",
      "capacity": 256
    }
    ```

    <div class="cfg-fields" markdown>

    ### `capacity`

    <span class="cfg cfg-type">integer</span>
    <span class="cfg cfg-def">Default: <code>256</code></span>

    How many recent records to keep in memory, for a Go program embedding
    Apranik to read.

    </div>

=== "syslog"

    ```json
    {
      "type": "syslog",
      "network": "udp",
      "address": "10.0.0.2:514",
      "tag": "apranik"
    }
    ```

    <div class="cfg-fields" markdown>

    ### `network`, `address`

    <span class="cfg cfg-type">string</span>
    <span class="cfg cfg-def">Default: the local daemon</span>

    Where the syslog daemon listens. Both empty means the local one.

    ### `tag`

    <span class="cfg cfg-type">string</span>
    <span class="cfg cfg-def">Default: the program name</span>

    The tag on each message.

    </div>

    Not available on Windows or Plan 9; there it is refused at startup.

## Live view for an API

An [API](api/index.md) can subscribe to the log at its own level, without any
output configured. With no subscriber this costs nothing. A subscriber that falls
behind loses records and is told how many.
