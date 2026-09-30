---
hide:
  - navigation
---

# Apranik

<div class="hero" markdown>

<p class="tagline">An open-source proxy core for censorship circumvention, written in Go.
Client apps and server panels run the <code>apranik</code> binary with a config file,
as they run Xray-core or sing-box.</p>

Its one design goal is speed of adaptation: a researcher who finds a new
circumvention technique should be able to ship it as **one module** (one folder
and one registry line) without touching anything else.

</div>

<div class="grid cards" markdown>

-   :material-rocket-launch-outline:{ .lg .middle } **Get started**

    ---

    What Apranik is, how to run it, and how to write a config file for it.

    [:octicons-arrow-right-24: Overview](overview.md) ·
    [Command line](cli.md) ·
    [Configuration](configuration/index.md) ·
    [Examples](configuration/examples.md)

-   :material-sitemap-outline:{ .lg .middle } **How it works**

    ---

    The layers, the `spec` contract, and one request traced end to end.

    [:octicons-arrow-right-24: Architecture](architecture.md) ·
    [Request lifecycle](request-lifecycle.md)

-   :material-puzzle-outline:{ .lg .middle } **Write a module**

    ---

    Add a transport, a proxy protocol or a sniffer in one folder.

    [:octicons-arrow-right-24: Contributing guide](contributing.md) ·
    [Good first issues](contributors/good-first-issues.md)

-   :material-scale-balance:{ .lg .middle } **Design**

    ---

    The decisions taken, the questions still open, and the threat model.

    [Open questions](design/open-questions.md)

</div>

## The system at a glance

[![System overview: a JSON config file is parsed and checked into a blueprint, which the builder turns into inbounds, outbounds and a router. At run time a client connects to an inbound, the handler asks the router, which runs the routing rules and picks an outbound or blocks, and the relay copies traffic to the destination.](assets/diagrams/system-overview.excalidraw.svg)](assets/diagrams/system-overview.excalidraw.svg)


## Status at a glance

| Area | Implemented today |
| --- | --- |
| Transports | `tcp` (base stream), `udp` (base datagram), `tls`, `ws` and `httpupgrade` (stream overlays) |
| Proxy protocols | `socks5` (CONNECT, UDP ASSOCIATE, username/password), `http` (CONNECT, absolute-form, Basic auth), `trojan` (CONNECT and UDP in the stream, named users, fallback for refused requests) |
| Outbounds | proxy outbound over a stream transport stack, TCP and (for Trojan) UDP; `freedom` (straight to the destination, TCP and UDP) |
| Sniffers | `tls` (SNI), `http` (Host and port); run by a routing rule's `sniff` action, only when a request reaches it |
| Routing | ordered `{match, action}` rules with `any`/`all`/`not`; domain, IP, port, source, inbound, user and sniffed-protocol fields; `route`, `block` and `sniff` actions; datasets from V2Ray's `geosite.dat` and `geoip.dat` |
| Configuration | versioned JSON config file, checked in full before anything starts |
| Interop | Trojan over `ws`, `httpupgrade` and `tls` tested by hand against Xray 26.7, both directions, TCP and UDP |

Not built yet: multiplexing transports (HTTP/2, gRPC), VLESS, DNS (and resolving
for IP rules), outbound groups, UDP inbounds, UDP sniffing and QUIC. For a working setup, start with the
[deployment examples](configuration/examples.md).
