# PROXY protocol

An overlay stream transport, **inbound only**: it reads the PROXY protocol
header (HAProxy's, versions 1 and 2) that a load balancer or proxy writes
before the client's own bytes, and reports the client it names as the
connection's source address. HAProxy (`send-proxy`), nginx (`proxy_protocol on`
in a `stream` block), AWS load balancers, Cloudflare Spectrum and others send it. It
works for any protocol, including ones the proxy cannot read, which is why it
exists next to [`original_source_ip`](original_source_ip.md).

It is not a [proxy protocol](../protocol/socks5.md) such as SOCKS5; the name is
HAProxy's.

## Structure

```json
{
  "transport": [
    { "type": "tcp" },
    { "type": "proxy_protocol", "settings": { "verification": { "trusted_proxy_addresses": ["10.0.0.2"] } } },
    { "type": "tls", "settings": { "certificates": [{ "certificate_file": "cert.pem", "key_file": "key.pem" }] } },
    { "type": "ws" }
  ]
}
```

> [!IMPORTANT]
> It must come **directly after `tcp`**, before any other overlay: it reads the
> first bytes of the connection. Placed above `tls` it sees decrypted bytes, no
> header arrives, and every connection fails with an error that says so. Nothing
> checks the position when the configuration is read, as with
> [`fragment`](fragment.md).

<div class="cfg-fields" markdown>

### `verification`

<span class="cfg cfg-type">object</span>

| Field | Meaning |
| --- | --- |
| `trusted_proxy_addresses` | Addresses (`"10.0.0.2"`) and ranges (`"10.0.0.0/8"`) of the proxies allowed to connect. Default **`127.0.0.0/8` and `::1`**: only a proxy on this machine. Every peer must send the header, and a peer **outside the list is refused**. `"0.0.0.0/0"` and `"::/0"` accept every peer, with a warning. An empty list is refused. |

### `timeout`

<span class="cfg cfg-type">duration</span>
<span class="cfg cfg-def">Default: <code>1s</code></span>

How long a peer may take to send the header. The sender writes it as soon as it
connects and waits for no answer, so a second is generous. This is the one
timeout not called `handshake_timeout` ([why](../timeouts.md)). The error names
the timeout, so a slow sender is easy to spot.

</div>

## What it does

- Both versions are detected from the first bytes; there is nothing to choose.
- The client's address and port become the source. The address the client
  reached at the proxy is kept alongside (`Metadata.Transport.ProxyProtocol`).
- A header that names no client (`LOCAL`, used for health checks, `UNKNOWN`,
  `UNSPEC`, unix or datagram addresses) is accepted, and the source stays the
  peer.
- v2 extensions (ALPN, authority, unique id) are skipped.
- A connection with no valid header is closed, with an error saying so; a
  refused or closed connection is **not** passed to the inbound's `fallback`,
  which runs after the overlays. The error is logged at `debug`, since scanners
  trigger it constantly: set the log level to `debug` the first time you connect
  a proxy.

An overlay above it that checks the address, such as `ws` with
`trusted_proxy_addresses`, sees the address this one reported. In a chain of a CDN,
a load balancer and Apranik, this layer lists the load balancer and `ws` lists
the CDN.

## Warnings

`apranik check` warns when the list covers every address, since any client could
then claim any source. The default (this machine only) never warns.
