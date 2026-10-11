// Package muxcool is Mux.Cool, the multiplexing format of V2Ray and Xray: many
// independent sub-connections carried over one established reliable stream, the
// main connection. VMess, VLESS and Shadowsocks can enter it (VLESS with
// command 3, the others with the destination "v1.mux.cool"); this package knows
// nothing of them. The carrier is handed in, and nothing here dials.
//
//	frame      = LENGTH(2)  METADATA(LENGTH)  [ DATA-LENGTH(2)  DATA ]
//	METADATA   = ID(2)  STATUS(1)  OPTIONS(1)  EXTRA
//	STATUS     = 1 New | 2 Keep | 3 End | 4 KeepAlive
//	OPTIONS    = bit 0 (1) data follows | bit 1 (2) error
//	New   EXTRA = NETWORK(1: 1 TCP, 2 UDP)  PORT(2)  TYPE(1)  ADDRESS     (see vmessaddr)
//	Keep  EXTRA = nothing, or for UDP: NETWORK(1)=2  PORT(2)  TYPE(1)  ADDRESS
//	End, KeepAlive carry no EXTRA.
//
// The format is symmetric: both ends send and read the same frames, and a
// reply carries the ID of its request. A client opens a sub-connection with New
// (which may carry its first data), moves data in Keep and ends it with End; a
// server never sends New. A Keep for an ID that is not open is answered with
// End, an End for one is ignored, and an unknown status closes the main
// connection. KeepAlive keeps a main connection from being closed as idle.
//
// The layouts and behaviours here follow the Xray documentation of Mux.Cool, the
// v2fly original, Xray-core (common/mux) and sing-vmess; docs/plans/mux-cool.md
// lists where they differ and what this package does about each.
//
// # What to know before using it
//
//   - A duplicate ID closes the main connection. A New for an ID that is open is
//     a protocol error, because every frame is addressed by ID alone: after a
//     second New nothing tells the two conversations apart, and any answer would
//     harm the old one or mix them. HTTP/2 does the same for a reused stream ID.
//     This client never reuses an ID, and a correct Xray or sing-box never does
//     either.
//   - Refusing one sub-connection keeps the main connection. A New over a limit,
//     for a network the server does not accept, or one the caller turned away is
//     answered with an End carrying the error bit for that ID.
//   - There is no half-close. End ends a sub-connection in both directions, so
//     CloseWrite returns spec.ErrNoHalfClose.
//   - There is no flow control. The reader reads a frame's data straight into
//     the caller's slice when the caller reads, so a sub-connection nobody reads
//     holds up every other sub-connection of its main connection.
//   - The loss of the peer is noticed when the reader of the main connection next
//     reads the carrier. While it waits for a sub-connection to read its data it
//     reads nothing, so a peer that vanished then is noticed when that
//     sub-connection reads or closes.
//   - A main connection with no sub-connection for IdleTimeout (60 s for a
//     server end, 15 s for a client end) is closed.
//   - Mux is for handshake latency, not throughput: one lost packet stalls all
//     sub-connections, which is why Xray's documentation advises against it for
//     downloads.
package muxcool
