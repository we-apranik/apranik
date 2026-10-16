# Stats

What Apranik counts: traffic per inbound, outbound and user, failures by reason,
routing rule hits and the resolver's work. Counting is on by default; this section
narrows it or turns on the parts that cost extra. The numbers are read through an
[API](../api/index.md). See [Counters](counters.md) for what each one means.

## Structure

<div class="cfg-structure">

```json
{
  "stats": {
    "enabled": true,
    "inbounds": ["socks-in"],
    "outbounds": ["proxy"],
    "dial_health": false,
    "tracking": {
      "enabled": true,
      "udp_entry_idle_timeout": "1m",
      "udp_max_entries_per_session": 1024
    }
  }
}
```

</div>

<div class="cfg-fields">

## Fields

### `enabled`

<span class="cfg cfg-type">boolean</span>
<span class="cfg cfg-def">Default: <code>true</code></span>

Count anything at all. `false` turns off every key below.

### `inbounds`

<span class="cfg cfg-type">list of tags</span>
<span class="cfg cfg-def">Default: all</span>

Count only these inbounds.

### `outbounds`

<span class="cfg cfg-type">list of tags</span>
<span class="cfg cfg-def">Default: all</span>

Count only these outbounds. Every authenticated user is always counted.

### `dial_health`

<span class="cfg cfg-type">boolean</span>
<span class="cfg cfg-def">Default: <code>false</code></span>

Time every dial of every outbound.

### `tracking.enabled`

<span class="cfg cfg-type">boolean</span>
<span class="cfg cfg-def">Default: on when <code>apis</code> is set</span>

Keep the live list of open TCP connections and UDP destinations, so an API can list
and close them and show who is online. About 350 bytes per entry.

### `tracking.udp_entry_idle_timeout`

<span class="cfg cfg-type">duration</span>
<span class="cfg cfg-def">Default: <code>1m</code></span>

A UDP destination leaves the live list after this much silence either way.

### `tracking.udp_max_entries_per_session`

<span class="cfg cfg-type">integer</span>
<span class="cfg cfg-def">Default: <code>1024</code></span>

The most destinations one UDP session lists; the one used longest ago goes first.

</div>
