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

    What Apranik is, and how to write a config file for it.

    [:octicons-arrow-right-24: Overview](overview.md) ·
    [Configuration](configuration.md)

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

[![System overview: a JSON config file is parsed and checked into a blueprint, which the builder turns into inbounds and outbounds. At run time a client connects to an inbound, the handler asks the router for an outbound and the relay copies traffic to the destination.](assets/diagrams/system-overview.excalidraw.svg)](assets/diagrams/system-overview.excalidraw.svg)


## Status at a glance

| Area | Implemented today |
| --- | --- |
| Transports | `tcp` (base stream), `udp` (base datagram) |
| Proxy protocols | `socks5` (CONNECT, UDP ASSOCIATE, username/password), `http` (CONNECT, absolute-form, Basic auth), `trojan` (CONNECT and UDP in the stream, named users, fallback for refused requests; plaintext until TLS exists) |
| Outbounds | proxy outbound over a stream transport stack; `freedom` (straight to the destination, TCP and UDP); `block` (refuses everything) |
| Sniffers | `tls` (SNI), `http` (Host and port); per inbound, off by default |
| Routing | the interface and the call sites exist; there are no rules yet, so every request takes the first outbound |
| Configuration | versioned JSON config file, checked in full before anything starts |

Not built yet: overlay transports such as TLS or WebSocket, routing rules, UDP
inbounds, UDP sniffing and QUIC.
