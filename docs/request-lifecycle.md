# Request lifecycle

This page follows real traffic through the code: first one TCP connection through
a SOCKS5 inbound and the freedom outbound, then the same connection through a
Trojan-over-TLS proxy, then one UDP session opened by SOCKS5 UDP ASSOCIATE.
Every function named here exists in the repository today.

## Startup

`cmd/apranik/cli/run.go` does the wiring:

1. `load` calls `registry.Default()`, then `config.LoadFile` (or `config.LoadURL`),
   which parses the config file into a `blueprint.Blueprint`, runs `config.Check`,
   and reads the routing datasets with `config.CheckDatasets`.
2. `serve` creates a `builder.Builder` with `builder.New`, which starts logging.
3. Each outbound is built with `Builder.BuildOutbound`. A `freedom` protocol
   returns `outbound.NewFreedom`, and a proxy protocol returns
   `outbound.NewStream`.
4. `Builder.BuildRouter` loads the datasets (`dataset.LoadAll`) and compiles the
   rules with `router.New`, every tag already turned into its outbound or
   dataset. Without a `routing` section, `router.NewWithDefaultOutbound` sends
   everything to the first outbound. One router serves every inbound.
5. Each inbound is built with `Builder.BuildInbound` and started with
   `Activate(ctx, handler.New(router, handler.Config{...}))`, the config carrying
   that inbound's `tcp_idle_timeout` and `udp_session_idle_timeout`.

## One TCP connection

[![Sequence of one TCP connection: the client connects to core/inbound, the socks5 server negotiates and returns the payload stream and Metadata, and core/inbound hands them to the handler. The handler calls Route with an informant; the router walks the rules and, only if a rule's action is sniff, asks the informant, which has core/sniff peek at the first bytes and replay them. The router returns a Decision, an outbound or block. For an outbound, freedom dials the destination through the tcp module and core/relay copies bytes both ways with half-close.](assets/diagrams/tcp-lifecycle.excalidraw.svg)](assets/diagrams/tcp-lifecycle.excalidraw.svg)

Step by step:

1. **Accept.** `streamInbound.acceptLoop` (in `core/inbound/inbound.go`) calls the
   base listener's `AcceptStream`, takes a `max_connections` slot, and starts
   `handleStream` in a goroutine for each connection.
2. **Handshake.** `handleStream` bounds the handshake with `handshakeContext`
   (default 10 seconds). If the inbound's stack has overlays,
   `wrapServerOverlays` runs each one's `WrapServerStream` first, in stack order
   (a TLS handshake, a WebSocket upgrade); a failure is logged at Debug and the
   connection closed. Core made a `spec.TransportMetadata` when it accepted the
   socket and hands every overlay a copy: an overlay that learns the real client
   replaces `Source`, and core keeps nothing else it writes. Then it calls the proxy server's `AcceptStream`. For SOCKS5
   this is `streamServer.AcceptStream` in `module/proxy/socks5/server.go`: it
   authenticates, reads the CONNECT request, replies, and returns the same stream
   as the payload plus a `Metadata` with the destination.
   If the protocol can also frame datagrams in the stream, as Trojan can, the
   inbound first asks `IsDatagramOverStream`, which only peeks, and calls
   `AcceptDatagramOverStream` instead when the request asks for UDP. If the
   protocol refuses the request with `spec.ErrFallbackRequested` and the inbound
   has a `fallback`, the inbound replays every byte read so far to that address
   and relays, and the steps below do not happen.
3. **Route.** The inbound hands the payload and `Metadata` to
   `Handler.HandleStream` in `core/route/handler/handler.go`, which wraps them
   in a `streamTrafficInformant` and calls `Router.Route`. The router walks the
   rules in order and returns a `Decision`: an outbound, or block, which closes
   the stream.
4. **Sniff (when a rule asks).** A rule whose action is `sniff` calls the
   informant's `Sniff`. Only then does the handler make the payload a
   `peekable.Stream`; `sniff.Recognise` peeks at the first bytes with a read
   deadline and shows each sniffer everything buffered. Nothing is consumed:
   the handler relays from the same wrapper, so the peeked bytes are replayed.
   The result goes into `Metadata.Sniffed` for the later rules; the
   destination is not changed.
5. **Dial.** `freedomOutbound.DialStream` in `core/outbound/freedom.go` passes the
   destination to the stream dialer. The TCP module's `streamDialer.DialStream`
   bounds the connect with its own `dial_timeout`; if the destination is a
   domain, this is the only place it is resolved, through the resolver
   (`core/dns`), and its addresses are dialed with Happy Eyeballs.
   For a proxy outbound, `streamOutbound.DialStream` instead dials the proxy
   server and calls the proxy client's `RequestStream`, which bounds itself with
   the `handshake_timeout` in its own settings. This is where the meaning of the address flips from
   "destination" to "proxy server".
6. **Relay.** `relay.Stream` in `core/relay/stream.go` copies both directions with
   `forwardStream`, calling `CloseWrite` on the far side when one side finishes.
   Cancelling the context closes both streams, and so does silence in both
   directions for the inbound's `tcp_idle_timeout` (default 5 minutes).

## One UDP session (SOCKS5 UDP ASSOCIATE)

[![Sequence of one UDP session: the client sends UDP ASSOCIATE, socks5 hands off with ErrUDPAssociationRequested, binds the second socket and replies; the handler creates the session and runs relay.Datagrams. Each packet asks the routing cache, which asks the router on a miss; the cached decision is an outbound or a block. Session.Send writes through freedom's NAT; replies come back through the session to the client. Closing the control connection ends the session.](assets/diagrams/udp-lifecycle.excalidraw.svg)](assets/diagrams/udp-lifecycle.excalidraw.svg)

Step by step:

1. **Hand-off.** SOCKS5's `AcceptStream` cannot return a `Datagram`, so for UDP
   ASSOCIATE it returns `spec.ErrUDPAssociationRequested`. `handleStream` then calls
   `handleUDPAssociate`, which calls `streamServer.Associate`. That opens the second
   socket through the `spec.UDPAssociateSocketFunc` the builder granted
   (`Builder.buildUDPAssociateSocket`), replies with its address, and returns.
2. **Lifetime.** `handleUDPAssociate` starts a goroutine that reads the control
   stream until it closes, then closes the association. The control connection
   bounds the UDP session.
3. **Session.** `Handler.HandleDatagram` creates a `session.Session` and a
   `udpSessionRoutingCache`, then runs `relay.Datagrams`.
4. **Client → destination.** `forwardClientDatagramsToSession` reads a packet,
   asks `OutboundForDestination` (the cache's `route` method, which calls
   `Handler.routeUDPDestination` on a miss with a `udpSessionTrafficInformant`,
   which sniffs nothing), then calls `Session.Send`. The cache keeps each
   destination's decision, block included. A packet that is blocked, or cannot
   be routed or sent, is dropped and logged at Debug; the session continues.
5. **Opening an outbound datagram.** On the first send through an outbound,
   `Session.openOrReuseOutboundDatagram` calls the outbound's `CreateDatagram`. For
   freedom that is `nat.New`, a `nat.Datagram` that opens real sockets lazily in
   `WriteTo` according to the mapping policy.
6. **Replies.** `Session.forwardRepliesFrom` reads each outbound datagram and pushes
   replies into one channel. `forwardSessionRepliesToClient` writes them back to
   the client. A reply to a domain is reported under the domain the client used.
7. **End.** The session ends when the client side closes, on `Close`, or after the
   inbound's `udp_session_idle_timeout` of silence (default 5 minutes). Outbound
   datagrams live as long as the session; within freedom, each NAT socket closes
   after the outbound's `nat.idle_timeout`.

## Through a proxy outbound over TLS

On the client side, a Trojan-over-TLS outbound replaces step 5:

1. `streamOutbound.DialStream` (in `core/outbound/proxy.go`) calls its dialer
   with the proxy server's address; each layer bounds its own part. The builder made
   that dialer from the base and the overlays: `overlayStreamDialer.DialStream`
   in `core/builder/builder.go` dials TCP, then calls the TLS client's
   `WrapClientStream`, which handshakes with the server's domain as SNI.
2. The Trojan client's `RequestStream` returns at once; it sends the request
   together with the first payload, or alone after 100ms.
3. On the server, the TLS inbound's `WrapServerStream` handshakes in that
   connection's goroutine; Trojan's `IsDatagramOverStream` peeks at the command
   byte, and `AcceptStream` checks the password and reads the destination. The
   rest is steps 3–6 above, on the server.

For UDP, `streamOutbound.CreateDatagram` does the same dial and calls
`RequestDatagramOverStream`: every datagram of the UDP session is framed inside
that one stream, and the server's `AcceptDatagramOverStream` hands the handler a
`Datagram`. Without a datagram-over-stream kind (SOCKS5 and HTTP as clients),
the outbound's `Networks()` lists only `tcp`, and `CreateDatagram` returns
`spec.ErrUnsupportedNetwork`: a UDP packet routed there is dropped and logged at
Debug.
