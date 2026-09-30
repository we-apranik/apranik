# Sniffing

Sniffing reads the start of a connection to learn what it carries: the server
name of a TLS connection, or the host of an HTTP request. It is a
[routing action](index.md#sniff): it runs only when a request reaches that
rule, and later rules see the result. The destination itself is never
rewritten; the connection still goes where the client asked.

## Structure

<div class="cfg-structure" markdown>

```json
{
  "match": { "inbound": ["mixed-in"], "network": "tcp" },
  "action": {
    "type": "sniff",
    "settings": {
      "sniffers": [{ "type": "tls" }, { "type": "http" }],
      "timeout": "300ms"
    }
  }
}
```

</div>

Sniffing works on TCP only. A connection is sniffed at most once, however many
`sniff` rules it reaches.

[![Sniffing: a routing rule's sniff action asks the traffic informant to sniff. On the first sniff of a connection the handler wraps the stream in a peekable.Stream, sets a read deadline (the action's timeout, default 300ms), reads up to 16 KiB and asks each undecided sniffer; ErrSniffNeedMoreData reads more, other errors drop the sniffer, a result wins. It then clears the deadline and remembers the result, and the router sets Metadata.Sniffed for the later rules. A second sniff returns the remembered result. The destination is never rewritten.](../../assets/diagrams/sniffing-flow.excalidraw.svg)](../../assets/diagrams/sniffing-flow.excalidraw.svg)

<div class="cfg-fields" markdown>

## Fields

### `sniffers`

<span class="cfg cfg-type">list of modules</span>
<span class="cfg cfg-req">Required</span>

The sniffers to run, at least one. The same sniffer twice is accepted with a
warning.

### `timeout`

<span class="cfg cfg-type">duration</span>
<span class="cfg cfg-def">Default: <code>300ms</code></span>

How long to wait for enough of the connection to recognise it. A client that
waits for the server to speak first, as SSH and SMTP do, waits this long. Put
rules that do not need a name, such as `{"port": [22]}`, before the `sniff`
rule. Must not be negative; over 5 seconds is accepted with a warning.

</div>

## What rules see

- [`sniffed_protocol`](index.md#the-request) reads the protocol: `tls` or
  `http`.
- [Domain fields](index.md#domains) read the sniffed name when there is one,
  and the destination's domain otherwise.

## Sniffers

Neither takes settings yet.

### `tls`

Reads the server name (SNI) from a TLS ClientHello.

### `http`

Reads the `Host` header, and its port, from an HTTP/1 request.
