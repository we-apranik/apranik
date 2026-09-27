# Freedom

`freedom` sends each connection straight to its destination, over plain TCP and
UDP. It is a special outbound protocol, built by core.

## Structure

<div class="cfg-structure" markdown>

```json
{
  "tag": "direct",
  "protocol": {
    "type": "freedom",
    "settings": {
      "tcp": { "keep_alive": "15s" },
      "udp": { "resolve_timeout": "5s" },
      "nat": {
        "mapping": "endpoint-independent",
        "filtering": "address-and-port-dependent",
        "idle_timeout": "5m"
      }
    }
  },
  "dial_timeout": "10s"
}
```

</div>

Only [`tag`](index.md#tag) and [`dial_timeout`](index.md#dial_timeout) apply of
the [outbound fields](index.md); freedom takes no server and no transport.

<div class="cfg-fields" markdown>

## Fields

### `tcp`

<span class="cfg cfg-type">object</span>
<span class="cfg cfg-def">Default: the module's defaults</span>

[`tcp` transport settings](../transport/tcp.md) for its TCP connections.

### `udp`

<span class="cfg cfg-type">object</span>
<span class="cfg cfg-def">Default: the module's defaults</span>

[`udp` transport settings](../transport/udp.md) for its UDP sockets.

### `nat`

<span class="cfg cfg-type">object</span>

How its UDP behaves as a NAT, in RFC 4787 terms.

### `nat.mapping`

<span class="cfg cfg-type">string</span>
<span class="cfg cfg-def">Default: <code>endpoint-independent</code></span>

When a UDP session gets a new socket: `endpoint-independent` (one socket for every
destination), `address-dependent` (one per destination IP) or
`address-and-port-dependent` (one per IP and port).

### `nat.filtering`

<span class="cfg cfg-type">string</span>
<span class="cfg cfg-def">Default: <code>address-and-port-dependent</code></span>

Which senders may reply: `address-and-port-dependent` (only the exact address and
port sent to), `address-dependent` (any port of an IP sent to) or
`endpoint-independent` (anyone).

> [!WARNING]
> `endpoint-independent` filtering lets an off-path sender inject packets into
> the session once a mapping exists. It is accepted with a warning.

### `nat.idle_timeout`

<span class="cfg cfg-type">duration</span>
<span class="cfg cfg-def">Default: <code>5m</code></span>

Closes a socket that has been silent this long. Must be positive.

</div>

[![RFC 4787 in freedom's NAT. Mapping: endpoint-independent (default) uses one socket for every destination, address-dependent one per destination IP, address-and-port-dependent one per IP and port. Filtering on a socket that sent to 1.1.1.1:53: endpoint-independent accepts anyone, address-dependent accepts any port of 1.1.1.1, address-and-port-dependent (default) accepts only 1.1.1.1:53.](../../assets/diagrams/nat-behaviours.excalidraw.svg)](../../assets/diagrams/nat-behaviours.excalidraw.svg)
