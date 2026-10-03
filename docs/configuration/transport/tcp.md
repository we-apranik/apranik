# TCP

The base stream transport.

## Structure

<div class="cfg-structure" markdown>

```json
{
  "type": "tcp",
  "settings": {
    "keep_alive": "15s",
    "keep_alive_interval": "15s",
    "keep_alive_count": 9,
    "no_delay": true
  }
}
```

</div>

<div class="cfg-fields" markdown>

## Fields

### `keep_alive`

<span class="cfg cfg-type">duration</span>
<span class="cfg cfg-def">Default: <code>15s</code></span>

How long a connection is idle before the first keep-alive probe. Negative
disables keep-alive.

### `keep_alive_interval`

<span class="cfg cfg-type">duration</span>
<span class="cfg cfg-def">Default: <code>15s</code></span>

Time between probes.

### `keep_alive_count`

<span class="cfg cfg-type">integer</span>
<span class="cfg cfg-def">Default: <code>9</code></span>

Unanswered probes before the connection is dropped.

### `no_delay`

<span class="cfg cfg-type">boolean</span>
<span class="cfg cfg-def">Default: <code>true</code></span>

Send small writes at once instead of batching them (disables Nagle's algorithm).

### `dial_timeout`

<span class="cfg cfg-type">duration</span>
<span class="cfg cfg-def">Default: <code>10s</code></span>

How long connecting may take, name resolution included. Outbound only. It
bounds the TCP connection alone; each layer above bounds its own handshake
with its own `handshake_timeout`.

</div>
