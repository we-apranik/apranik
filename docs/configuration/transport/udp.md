# UDP

The base datagram transport. Today it is configured only inside
[`freedom`](../outbound/freedom.md#udp)'s settings.

## Structure

<div class="cfg-structure" markdown>

```json
{
  "udp": {
    "resolve_timeout": "5s",
    "resolve_cache_ttl": "30s"
  }
}
```

</div>

<div class="cfg-fields" markdown>

## Fields

### `resolve_timeout`

<span class="cfg cfg-type">duration</span>
<span class="cfg cfg-def">Default: <code>5s</code></span>

How long looking up a destination domain may take. Must not be negative.

### `resolve_cache_ttl`

<span class="cfg cfg-type">duration</span>
<span class="cfg cfg-def">Default: <code>30s</code></span>

How long a looked-up address is reused. Must not be negative.

</div>
