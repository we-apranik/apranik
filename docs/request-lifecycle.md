# Request lifecycle

This page follows real traffic through the code: first one TCP connection through
a SOCKS5 inbound and the freedom outbound, then one UDP session opened by SOCKS5
UDP ASSOCIATE. Every function named here exists in the repository today.

## Startup

`cmd/apranik/cli/run.go` does the wiring:

1. `load` calls `registry.Default()`, then `config.LoadFile` (or `config.LoadURL`),
   which parses the config file into a `blueprint.Blueprint` and runs `config.Check`.
2. `serve` creates a `builder.Builder` with `builder.New`, which starts logging.
3. Each outbound is built with `Builder.BuildOutbound`. A `freedom` protocol
   returns `outbound.NewFreedom`, `block` returns `outbound.NewBlock`, and a proxy
   protocol returns `outbound.NewStream`.
4. There are no routing rules yet, so the first outbound becomes the default:
   `router.New(defaultOutbound)`.
5. Each inbound is built with `Builder.BuildInbound` and started with
   `Activate(ctx, handler.New(router, handler.Config{...}))`, the config carrying
   that inbound's `tcp_idle_timeout` and `udp_session_idle_timeout`.

## One TCP connection

[![Sequence of one TCP connection: the client connects to core/inbound, the socks5 server negotiates and returns the payload stream and Metadata, core/sniff optionally peeks and replays, the handler asks the router, freedom dials the destination through the tcp module, and core/relay copies bytes both ways with half-close.](assets/diagrams/tcp-lifecycle.excalidraw.svg)](assets/diagrams/tcp-lifecycle.excalidraw.svg)

Step by step:

1. **Accept.** `streamInbound.acceptLoop` (in `core/inbound/inbound.go`) calls the
   listener's `AcceptStream` and starts `handleStream` for each connection.
2. **Handshake.** `handleStream` bounds the handshake with `handshakeContext`
   (default 10 seconds) and calls the proxy server's `AcceptStream`. For SOCKS5
   this is `streamServer.AcceptStream` in `module/proxy/socks5/server.go`: it
   authenticates, reads the CONNECT request, replies, and returns the same stream
   as the payload plus a `Metadata` with the destination.
   If the protocol can also frame datagrams in the stream, as Trojan can, the
   inbound first asks `IsDatagramOverStream`, which only peeks, and calls
   `AcceptDatagramOverStream` instead when the request asks for UDP. If the
   protocol refuses the request with `spec.ErrFallbackRequested` and the inbound
   has a `fallback`, the inbound replays every byte read so far to that address
   and relays, and the steps below do not happen.
3. **Sniff (optional).** If the inbound has sniffers, `sniff.Stream` peeks at the
   first bytes with a read deadline, asks each sniffer, and returns a stream that
   replays what it read. The result goes into `metadata.Sniffed`; the destination is
   not changed.
4. **Route.** `Handler.HandleStream` in `core/route/handler/handler.go` calls
   `router.Route`. Today `Router.Route` returns the default outbound.
5. **Dial.** `freedomOutbound.DialStream` in `core/outbound/freedom.go` passes the
   destination to the stream dialer, bounded by the outbound's `dial_timeout`. The TCP module's `streamDialer.DialStream`
   joins host and port; if the destination is a domain, this is the only place
   it is resolved.
   For a proxy outbound, `streamOutbound.DialStream` instead dials the proxy
   server and calls the proxy client's `RequestStream`, bounded by the outbound's
   `handshake_timeout`. This is where the meaning of the address flips from
   "destination" to "proxy server".
6. **Relay.** `relay.Stream` in `core/relay/stream.go` copies both directions with
   `forwardStream`, calling `CloseWrite` on the far side when one side finishes.
   Cancelling the context closes both streams, and so does silence in both
   directions for the inbound's `tcp_idle_timeout` (default 5 minutes).

## One UDP session (SOCKS5 UDP ASSOCIATE)

[![Sequence of one UDP session: the client sends UDP ASSOCIATE, socks5 hands off with ErrUDPAssociationRequested, binds the second socket and replies; the handler creates the session and runs relay.Datagrams. Each packet asks the routing cache (the router on a miss), then Session.Send writes through freedom's NAT; replies come back through the session to the client. Closing the control connection ends the session.](assets/diagrams/udp-lifecycle.excalidraw.svg)](assets/diagrams/udp-lifecycle.excalidraw.svg)

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
   `Handler.routeUDPDestination` on a miss), then calls `Session.Send`. A packet
   that cannot be routed or sent is dropped and logged at Debug; the session
   continues.
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

> [!NOTE]
> A proxy outbound cannot carry UDP today: `streamOutbound.CreateDatagram`
> returns `spec.ErrUnsupportedNetwork`, because no implemented proxy protocol has a
> datagram kind. UDP leaves only through freedom.
