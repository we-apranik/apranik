# Sniffing

Sniffing reads the start of each connection to learn what it carries: the server
name of a TLS connection, or the host of an HTTP request. The result is recorded
for logs and future routing rules; the destination itself is never rewritten.

## Structure

<div class="cfg-structure" markdown>

```json
{
  "inbounds": [
    {
      "tag": "socks-in",
      "sniffing": {
        "sniffers": [{ "type": "tls" }, { "type": "http" }],
        "timeout": "300ms"
      }
    }
  ]
}
```

</div>

Sniffing is off unless the inbound has a `sniffing` object. It works on TCP
inbounds only.

<div class="cfg-fields" markdown>

## Fields

### `sniffers`

<span class="cfg cfg-type">list of modules</span>
<span class="cfg cfg-req">Required</span>

The sniffers to run, at least one. To turn sniffing off, remove the whole
`sniffing` object. The same sniffer twice is accepted with a warning.

### `timeout`

<span class="cfg cfg-type">duration</span>
<span class="cfg cfg-def">Default: <code>300ms</code></span>

How long to wait for enough of the connection to recognise it. Must not be
negative; over 5 seconds is accepted with a warning.

</div>

## Sniffers

Neither takes settings yet.

### `tls`

Reads the server name (SNI) from a TLS ClientHello.

### `http`

Reads the `Host` header, and its port, from an HTTP/1 request.
