# Fragment

An outbound overlay that cuts the TLS ClientHello into pieces, so DPI that reads
the server name (SNI) without reassembling never sees it whole. Only the
ClientHello, sent in the clear, is cut; nothing is decrypted, and the server
reassembles it unaware.

Put it directly below the layer that sends the ClientHello: under
[freedom](../outbound/freedom.md#transport), the user's own browser's; in a proxy
outbound, below `tls`.

[![What fragment does to a ClientHello: without it the whole ClientHello leaves in one packet and a DPI box reads the server name from it. With fragment the same bytes leave in several packets, with a cut inside the name, so DPI that inspects each packet alone cannot read it, while the server's TCP joins the packets back together.](../../assets/diagrams/fragment-cuts.excalidraw.svg)](../../assets/diagrams/fragment-cuts.excalidraw.svg)

## Structure

<div class="cfg-structure" markdown>

```json
{
  "type": "fragment",
  "settings": {
    "tls_records": true,
    "tcp_packets": true,
    "piece_sizes": ["3-5", "10-20"],
    "piece_delays": ["10-20ms"],
    "max_pieces": "4-8",
    "split_sni": true
  }
}
```

</div>

These are the defaults, so `{ "type": "fragment" }` behaves the same.

<div class="cfg-fields" markdown>

## Fields

### `tls_records`

<span class="cfg cfg-type">boolean</span>
<span class="cfg cfg-def">Default: <code>true</code></span>

Cut the ClientHello into several TLS records. Beats DPI that parses only the first
record.

### `tcp_packets`

<span class="cfg cfg-type">boolean</span>
<span class="cfg cfg-def">Default: <code>true</code></span>

Send the pieces as separate TCP packets. Beats DPI that inspects each packet alone.
At least one of the two must be on.

### `piece_sizes`

<span class="cfg cfg-type">list of ranges</span>
<span class="cfg cfg-def">Default: <code>["3-5", "10-20"]</code></span>

Piece sizes in bytes, such as `"5"` or `"3-5"`, picked per connection. The n-th
entry sizes the n-th piece; the last repeats.

### `piece_delays`

<span class="cfg cfg-type">list of duration ranges</span>
<span class="cfg cfg-def">Default: <code>["10-20ms"]</code></span>

The wait after each piece, by the same rule, such as `"10ms"` or `"10-20ms"`. Needs
`tcp_packets`. The defaults add about 70 to 140 ms to the TLS handshake.

### `max_pieces`

<span class="cfg cfg-type">range</span>
<span class="cfg cfg-def">Default: <code>"4-8"</code></span>

The most pieces, at least 2. The last piece takes what remains.

### `split_sni`

<span class="cfg cfg-type">boolean</span>
<span class="cfg cfg-def">Default: <code>true</code></span>

Always cut once inside the server name, so no piece holds it whole.

</div>

It does not beat DPI that reassembles connections, blocking by IP, TLS
fingerprinting, or SNI allow-lists.

## From Xray or sing-box

| Xray | sing-box | Apranik |
| --- | --- | --- |
| `packets: "tlshello"` | `tls_record_fragment` | `tls_records: true` |
| `delays` above 0 | `tls_fragment` | `tcp_packets: true` |
| `lengths` | – | `piece_sizes` |
| `delays` (ms) | – | `piece_delays`, with a unit: `"10-20ms"` |
| `maxSplit` | – | `max_pieces` |
