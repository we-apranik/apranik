# Freedom

`freedom` sends each connection straight to its destination, over TCP and UDP.
It is a special outbound protocol, built by core.

## Structure

<div class="cfg-structure" markdown>

```json
{
  "tag": "direct",
  "transport": [
    { "type": "tcp", "settings": { "keep_alive": "15s", "dial_timeout": "10s" } },
    { "type": "fragment" }
  ],
  "protocol": {
    "type": "freedom",
    "settings": {
      "nat": {
        "mapping": "endpoint-independent",
        "filtering": "address-and-port-dependent",
        "idle_timeout": "5m"
      }
    }
  }
}
```

</div>

Of the [outbound fields](index.md), [`tag`](index.md#tag) and `transport`
apply; freedom takes no server. How long a connection to a destination may take
is `tcp`'s [`dial_timeout`](../transport/tcp.md#dial_timeout). Freedom
resolves each destination's domain through the [resolver](../dns/index.md):
the operating system's when there is no `dns` section, the DNS rules' choice
when there is one.

## Transport

Freedom's `transport` is how its TCP connections reach each destination. Left
out, it is plain `tcp` with the module's defaults. It must start with
[`tcp`](../transport/tcp.md), and above it may hold only transports that need
no server side, such as [`fragment`](../transport/fragment.md): a destination
runs none of Apranik's layers, so anything it would have to undo, such as
`tls` or `ws`, would reach it as garbage. `check` refuses those and lists the
transports that do work:

```
outbounds[1].transport[1]: "ws" cannot be used with freedom. Freedom connects
straight to each destination, and a destination does not run "ws", so it would
receive ws framing instead of the client's traffic. Transports that work with
freedom: tcp, fragment.
```

`tcp`'s settings go in the stack, not in freedom's settings:
`[{ "type": "tcp", "settings": { "keep_alive": "15s" } }]`. Freedom takes no `settings.tcp`.

Freedom's UDP is not part of the stack; it is set in [`udp`](#udp).

<div class="cfg-fields" markdown>

## Fields

### `udp`

<span class="cfg cfg-type">object</span>
<span class="cfg cfg-def">Default: the module's defaults</span>

[`udp` transport settings](../transport/udp.md) for its UDP sockets. It has
none today: a domain a datagram is sent to is resolved through the
[resolver](../dns/index.md).

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

### `nat.max_datagram_size`

<span class="cfg cfg-type">integer, bytes</span>
<span class="cfg cfg-def">Default: <code>8192</code></span>

The largest reply the NAT passes on. A bigger one is dropped and counted as
`too_large_from_outbound` on this outbound; the socket keeps reading. Match the inbound's
[`udp_max_datagram_size`](../inbound/index.md#udp_max_datagram_size). `apranik check` refuses
more than 65,507.

### `nat.idle_timeout`

<span class="cfg cfg-type">duration</span>
<span class="cfg cfg-def">Default: <code>5m</code></span>

Closes a socket that has been silent this long. Must be positive.

</div>

[![RFC 4787 in freedom's NAT. Mapping: endpoint-independent (default) uses one socket for every destination, address-dependent one per destination IP, address-and-port-dependent one per IP and port. Filtering on a socket that sent to 1.1.1.1:53: endpoint-independent accepts anyone, address-dependent accepts any port of 1.1.1.1, address-and-port-dependent (default) accepts only 1.1.1.1:53.](../../assets/diagrams/nat-behaviours.excalidraw.svg)](../../assets/diagrams/nat-behaviours.excalidraw.svg)
