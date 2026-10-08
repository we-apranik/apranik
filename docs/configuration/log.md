# Log

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
> With no outputs, Apranik logs nothing. That is the default on purpose: a log
> naming what a user reached is evidence on a seized device. `apranik check` warns
> about it once.

<div class="cfg-fields" markdown>

## Fields

### `level`

<span class="cfg cfg-type">string</span>
<span class="cfg cfg-def">Default: <code>info</code></span>

The least severe level written. One of the eight RFC 5424 severities, from most
to least verbose: `debug`, `info`, `notice`, `warning`, `error`, `critical`,
`alert`, `emergency`.

### `outputs`

<span class="cfg cfg-type">list of outputs</span>
<span class="cfg cfg-def">Default: <code>[]</code>, silent</span>

Where records go. Every record goes to every output, so logs can go to several
places at once. Each output is an object whose `type` is one of the output types
below.

</div>

## Output types

=== "stderr"

    ```json
    { "type": "stderr", "format": "human", "color": true }
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

    The default suits the clients and panels that run Apranik and capture its
    stderr. Choose `human` when a person reads the terminal.

    ### `color`

    <span class="cfg cfg-type">boolean</span>
    <span class="cfg cfg-def">Default: <code>false</code></span>
    <span class="cfg cfg-note">human only</span>

    Color the level name and dim the time. Refused with any other format. When
    the `NO_COLOR` environment variable is set, color stays off whatever this
    says.

    </div>

=== "file"

    ```json
    { "type": "file", "path": "/var/log/apranik.log", "format": "json" }
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
    > A log file outlives the process. On a seized device it is evidence about
    > the person who ran Apranik. Write one only when you need it.

=== "memory"

    ```json
    { "type": "memory", "capacity": 256 }
    ```

    <div class="cfg-fields" markdown>

    ### `capacity`

    <span class="cfg cfg-type">integer</span>
    <span class="cfg cfg-def">Default: <code>256</code></span>

    How many recent records to keep; older ones are dropped. The records live
    only in memory, for a Go program embedding Apranik to read.

    </div>

=== "syslog"

    ```json
    { "type": "syslog", "network": "udp", "address": "10.0.0.2:514", "tag": "apranik" }
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

Besides the outputs above, the logging system always carries one more handler that nobody
configures: a hub an [API](api.md) can subscribe to for a live view of the log. With nobody
subscribed it is switched off, so a record is not even built for it, and **no outputs still
means silent**: nothing is kept, nothing is written.

A subscriber asks for its own level, which may be lower than the outputs use. While a
`debug` subscriber is connected, debug records exist and only it receives them; the file and
stderr still get what their level says. A subscriber that cannot keep up loses records, never
slows logging, and finds one warning in its stream saying how many were dropped and how big
its buffer was.
