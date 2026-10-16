# Probe

How a [`urltest`](../outbound/urltest.md) or [`fallback`](../outbound/fallback.md)
group tests its members: one HTTP request sent through each member, timed until the
status line arrives. Every member is probed when the core starts, then at each
`interval`. A probe bypasses routing and sends no `User-Agent`.

## Structure

<div class="cfg-structure">

```json
{
  "url": "https://www.gstatic.com/generate_204",
  "interval": "5m",
  "timeout": "10s",
  "expected_status": "200-299"
}
```

</div>

Every field is optional.

<div class="cfg-fields">

## Fields

### `url`

<span class="cfg cfg-type">string</span>
<span class="cfg cfg-def">Default: <code>https://www.gstatic.com/generate_204</code></span>

An `http://` or `https://` URL reachable from every member's exit. With `https`, the
time includes a TLS handshake made with Go's `crypto/tls`, not the
[`tls` fingerprint](../transport/tls.md#fingerprint).

### `interval`

<span class="cfg cfg-type">duration</span>
<span class="cfg cfg-def">Default: <code>5m</code></span>

Time from the end of one round to the start of the next.

### `timeout`

<span class="cfg cfg-type">duration</span>
<span class="cfg cfg-def">Default: <code>10s</code></span>

A member slower than this fails the round.

### `expected_status`

<span class="cfg cfg-type">string</span>
<span class="cfg cfg-def">Default: <code>200-299</code></span>

Statuses that pass: codes and inclusive ranges, separated by commas, such as
`"204"` or `"200,204,300-399"`. Write it as a string.

</div>
