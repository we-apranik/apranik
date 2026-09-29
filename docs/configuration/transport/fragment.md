# Fragment

An overlay stream transport that cuts the TLS ClientHello into pieces, so a
censor's DPI that reads the server name (SNI) from it without reassembling
never sees the name whole. The server reassembles the pieces and never knows.

The ClientHello is the one TLS message sent unencrypted; `fragment` cuts only
that, and passes everything after it through untouched. Nothing is decrypted,
and there are no keys involved. If the first bytes written are not a TLS
ClientHello, nothing is cut.

It has no server side: it only changes outgoing connections. It is refused on
an inbound.

## Where to put it

Directly below the layer that sends the ClientHello:

=== "Under freedom"

    Reaching SNI-blocked sites directly, with no server. The ClientHello cut
    is the user's own browser's.

    ```json
    {
      "tag": "direct",
      "transport": [
        { "type": "tcp" },
        { "type": "fragment" }
      ],
      "protocol": { "type": "freedom" }
    }
    ```

=== "In a proxy outbound"

    Hiding the SNI used to reach the proxy server or its CDN front.

    ```json
    {
      "server": "cdn.example.com",
      "server_port": 443,
      "transport": [
        { "type": "tcp" },
        { "type": "fragment" },
        { "type": "tls", "settings": { "server_name": "cdn.example.com", "alpn": ["http/1.1"] } },
        { "type": "ws", "settings": { "path": "/chat" } }
      ]
    }
    ```

## Structure

<div class="cfg-structure" markdown>

```json
{ "type": "fragment", "settings": {
    "tls_records":  true,
    "tcp_packets":  true,
    "piece_sizes":  ["3-5", "10-20"],
    "piece_delays": ["10-20ms"],
    "max_pieces":   "4-8",
    "split_sni":    true
} }
```

</div>

Every value above is the default, so `{ "type": "fragment" }` alone behaves
the same. The defaults are provisional, to be tuned from field tests.

## How it cuts

A ClientHello normally leaves as one TLS record in one TCP packet. The two
switches cut it at one layer or at both:

| `tls_records` | `tcp_packets` | On the wire | Beats DPI that |
| --- | --- | --- | --- |
| `true` | `false` | several small TLS records, all in one packet | joins packets, but parses only the first record |
| `false` | `true` | the one original record, carried in several packets | inspects each packet alone |
| `true` | `true` | several small records, each in its own packet | does either |

**Records** add a new 5-byte header to each piece; TLS allows a handshake
message to span records (RFC 8446 §5.1). They add no delay, since everything
still leaves in one write.

**Packets** change no bytes; the server's TCP joins them back. Each piece is
written separately, with a wait after it so the kernel does not merge it with
the next. That is why [`piece_delays`](#piece_delays) needs `tcp_packets`: with
records alone there is only one write, and nothing to wait between.

`piece_sizes` and `max_pieces` decide where the cuts go, and
[`split_sni`](#split_sni) adds one inside the server name. With both switches
on, the same cuts serve both layers.

<div class="cfg-fields" markdown>

## Fields

### `tls_records`

<span class="cfg cfg-type">boolean</span>
<span class="cfg cfg-def">Default: <code>true</code></span>

Cut the ClientHello into several TLS records.

### `tcp_packets`

<span class="cfg cfg-type">boolean</span>
<span class="cfg cfg-def">Default: <code>true</code></span>

Send the pieces as separate TCP packets. At least one of `tls_records` and
`tcp_packets` must be on.

### `piece_sizes`

<span class="cfg cfg-type">array of ranges</span>
<span class="cfg cfg-def">Default: <code>["3-5", "10-20"]</code></span>

Piece sizes in bytes. The n-th entry is the size of the n-th piece; the last
entry repeats for every piece after it. Each is `"5"` or `"3-5"` (a number
also works), picked at random for every connection. The default makes the
first piece tiny, which beats DPI that reads only the first few bytes. No size
may be 0: an empty TLS record breaks the RFC, and Go servers refuse it.

### `piece_delays`

<span class="cfg cfg-type">array of duration ranges</span>
<span class="cfg cfg-def">Default: <code>["10-20ms"]</code></span>

The wait after each piece, by the same rule: the n-th entry for the n-th
piece, the last repeating. Each is `"10ms"`, `"10-20ms"` (the unit applies to
both ends) or `"0"` (separate writes, no waiting). Only with `tcp_packets`.

Longer delays make separate packets more likely but slow the connection's
start: roughly the number of pieces times the delay, about 70–140 ms with the
defaults. In a proxy outbound this counts against `dial_timeout`.

### `max_pieces`

<span class="cfg cfg-type">range</span>
<span class="cfg cfg-def">Default: <code>"4-8"</code></span>

The most pieces the ClientHello is cut into, picked per connection; the last
piece takes all the remaining bytes. `0` means no limit. Without a limit, a
ClientHello of about 1.8 KB cut into 10–20 byte pieces becomes over a hundred
writes and waits.

### `split_sni`

<span class="cfg cfg-type">boolean</span>
<span class="cfg cfg-def">Default: <code>true</code></span>

Guarantee one cut inside the server name, at a random point, so no piece holds
it whole. It counts towards `max_pieces`. Random sizes alone often leave the
whole name inside one piece, where DPI that checks every piece still finds it.
Without a server name to find, the sizes alone decide.

</div>

## What `check` refuses

- `tls_records` and `tcp_packets` both `false`.
- `piece_delays` without `tcp_packets`.
- An empty `piece_sizes` or `piece_delays`, a size of 0, a malformed range or
  one whose minimum exceeds its maximum, a delay without a unit.
- `max_pieces` of 1: one piece splits nothing.
- `fragment` in an inbound's transport.

## What it does not beat

DPI that reassembles every connection before reading it; blocking by IP; TLS
fingerprinting; and SNI allow-lists, where an unreadable ClientHello may be
dropped. How well it works changes over time and is only known by testing on
the network in question.

## Coming from Xray or sing-box

| Xray `fragment` | Apranik |
| --- | --- |
| `packets: "tlshello"` | `tls_records: true` |
| a `delays` value above 0 | also `tcp_packets: true`, with the delays copied to `piece_delays` |
| all `delays` 0 | `tcp_packets: false` (records in one packet) |
| `lengths` | `piece_sizes` |
| `delays` (milliseconds) | `piece_delays`, with a unit: `"10-20"` becomes `"10-20ms"` |
| `maxSplit` | `max_pieces` |
| `packets: "1-3"` (any protocol) | not supported |

sing-box's `tls_record_fragment` is `tls_records`, and its `tls_fragment` is
`tcp_packets`. sing-box cuts only inside the server name; `split_sni` with
large `piece_sizes` comes closest. It waits for the server's acknowledgement
between packets rather than a fixed delay; `fragment` does not.
