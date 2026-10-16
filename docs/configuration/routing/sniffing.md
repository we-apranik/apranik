# Sniffing

The settings of a [`sniff`](action.md#sniff) action, and what each sniffer reports.
Sniffing reads the start of a TCP connection; the destination is never rewritten.
A connection is sniffed at most once.


## Structure

<div class="cfg-structure">

```json
{
  "type": "sniff",
  "settings": {
    "sniffers": [{ "type": "tls" }, { "type": "http" }],
    "timeout": "300ms"
  }
}
```

</div>

<div class="cfg-fields">

## Fields

### `sniffers`

<span class="cfg cfg-type">list of sniffers</span>
<span class="cfg cfg-req">Required</span>

The [sniffers](#sniffers) to run.

### `timeout`

<span class="cfg cfg-type">duration</span>
<span class="cfg cfg-def">Default: <code>300ms</code></span>

How long to wait for enough bytes to recognise the connection.

</div>

## Sniffers

Neither takes settings.

| Type | Reads | Reports |
| --- | --- | --- |
| `tls` | a TLS ClientHello | the server name (SNI) and a browser-family guess |
| `http` | an HTTP/1 request | the `Host` and every header, with `:method` and `:path` |

Later rules read the results through `sniffed_protocol`, `sniffed_request_header`,
`sniffed_browser_family` and the [domain fields](rule.md#domain).
