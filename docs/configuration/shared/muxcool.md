# Mux.Cool

The `muxcool` key of a [VLESS](../protocol/vless.md) inbound or outbound: how
[Mux.Cool](../../concepts/mux-cool.md) is used. On an outbound each key means
"send"; on an inbound, "accept". Left out, UDP goes as XUDP and TCP is not
multiplexed, as in Xray's and sing-box's clients.

## Structure

<div class="cfg-structure" markdown>

```json
{
  "udp": {
    "enabled": true,
    "xudp": {
      "enabled": true,
      "global_id": { "enabled": true, "hold_time": "60s" }
    }
  },
  "tcp": { "enabled": false }
}
```

</div>

<div class="cfg-fields" markdown>

## Fields

### `udp.enabled`

<span class="cfg cfg-type">boolean</span>
<span class="cfg cfg-def">Default: <code>true</code></span>

Carry UDP through Mux.Cool. `false` uses the protocol's plain UDP; on an inbound,
an Xray or sing-box client's default UDP then fails.

### `udp.xudp.enabled`

<span class="cfg cfg-type">boolean</span>
<span class="cfg cfg-def">Default: <code>true</code></span>

Use XUDP: one sub-connection carries every destination of a UDP session. `false`
is refused, since plain Mux.Cool UDP is not built.

### `udp.xudp.global_id.enabled`

<span class="cfg cfg-type">boolean</span>
<span class="cfg cfg-def">Default: <code>true</code></span>

Name each UDP session with a Global ID, so a session survives a lost stream (a
phone moving from Wi-Fi to mobile data): the destination keeps seeing the same
source port. Xray's server supports it; sing-box ignores it.

### `udp.xudp.global_id.hold_time`

<span class="cfg cfg-type">duration</span>
<span class="cfg cfg-def">Default: <code>60s</code></span>
<span class="cfg cfg-note">inbound only</span>

How long the server holds a session whose stream was lost. Only the same user can
take it back.

### `tcp.enabled`

<span class="cfg cfg-type">boolean</span>
<span class="cfg cfg-def">Default: <code>false</code></span>

TCP over Mux.Cool is not built: `true` is refused.

</div>

`udp.concurrency`, `tcp.concurrency` and `tcp.max_total` are reserved and refused.
