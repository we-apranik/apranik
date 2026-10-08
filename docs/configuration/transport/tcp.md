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
    "next_address_delay": "250ms",
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
disables keep-alive; a written `0` is refused.

### `keep_alive_interval`

<span class="cfg cfg-type">duration</span>
<span class="cfg cfg-def">Default: <code>15s</code></span>

Time between probes. Greater than zero.

### `keep_alive_count`

<span class="cfg cfg-type">integer</span>
<span class="cfg cfg-def">Default: <code>9</code></span>

Unanswered probes before the connection is dropped. From 1 to 255.

### `next_address_delay`

<span class="cfg cfg-type">duration</span>
<span class="cfg cfg-def">Default: <code>250ms</code></span>

When a domain has several addresses, how long one connection attempt runs
before the next starts beside it (Happy Eyeballs, RFC 8305). The
[resolver](../dns/index.md) orders the addresses by `ip_versions`; `tcp`
alternates IPv4 and IPv6 from the first, and the first connection to succeed
wins. A dead first address therefore costs this delay, not the whole
`dial_timeout`. Must be greater than zero. Outbound only.

### `no_delay`

<span class="cfg cfg-type">boolean</span>
<span class="cfg cfg-def">Default: <code>true</code></span>

Send small writes at once instead of batching them (disables Nagle's algorithm).

### `dial_timeout`

<span class="cfg cfg-type">duration</span>
<span class="cfg cfg-def">Default: <code>10s</code></span>

How long connecting may take, name resolution and every attempt included.
Outbound only. It
bounds the TCP connection alone; each layer above bounds its own handshake
with its own `handshake_timeout`.

</div>
