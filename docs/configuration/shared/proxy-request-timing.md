# Proxy request timing

The `proxy_request_timing` key of a [Trojan](../protocol/trojan.md) or
[VLESS](../protocol/vless.md) outbound. The client holds its proxy request until
the application's first bytes, so both leave in one write. A request sent alone is
a small packet of a fixed length, which DPI can recognise.

## Structure

<div class="cfg-structure">

```json
{
  "max_delay": "400ms",
  "skip": { "enabled": false, "ports": [21, 25, 110, 143, 587, 3306] }
}
```

</div>

<div class="cfg-fields">

## Fields

### `max_delay`

<span class="cfg cfg-type">duration</span>
<span class="cfg cfg-def">Default: <code>400ms</code></span>

The longest the request waits for the first bytes before it is sent alone.

### `skip.enabled`

<span class="cfg cfg-type">boolean</span>
<span class="cfg cfg-def">Default: <code>false</code></span>

Send the request at once for the ports in `skip.ports`. Their protocols (FTP, SMTP,
POP3, IMAP, MySQL) wait for the server to speak first, so otherwise they start
`max_delay` late. The cost is the recognisable lone request.

### `skip.ports`

<span class="cfg cfg-type">list of integers</span>
<span class="cfg cfg-def">Default: <code>[21, 25, 110, 143, 587, 3306]</code></span>

Destination ports that skip the wait. Writing it replaces the default list.

</div>

UDP is not affected: a UDP session sends its request with its first datagram.
