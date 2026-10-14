# TCP

The base stream transport: opens or accepts a TCP connection.

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
    "no_delay": true,
    "dial_timeout": "10s"
  }
}
```

</div>

<div class="cfg-fields" markdown>

## Fields

### `keep_alive`

<span class="cfg cfg-type">duration</span>
<span class="cfg cfg-def">Default: <code>15s</code></span>

Idle time before the first keep-alive probe. Negative disables keep-alive.

### `keep_alive_interval`

<span class="cfg cfg-type">duration</span>
<span class="cfg cfg-def">Default: <code>15s</code></span>

Time between keep-alive probes.

### `keep_alive_count`

<span class="cfg cfg-type">integer</span>
<span class="cfg cfg-def">Default: <code>9</code></span>

Unanswered probes before the connection is dropped, 1 to 255.

### `next_address_delay`

<span class="cfg cfg-type">duration</span>
<span class="cfg cfg-def">Default: <code>250ms</code></span>
<span class="cfg cfg-note">outbound</span>

When a domain has several addresses, how long one attempt runs before the next
starts beside it (Happy Eyeballs, RFC 8305). The first to connect wins.

### `no_delay`

<span class="cfg cfg-type">boolean</span>
<span class="cfg cfg-def">Default: <code>true</code></span>

Send small writes at once instead of batching them (disables Nagle's algorithm).

### `dial_timeout`

<span class="cfg cfg-type">duration</span>
<span class="cfg cfg-def">Default: <code>10s</code></span>
<span class="cfg cfg-note">outbound</span>

How long connecting may take, name resolution included.

</div>
