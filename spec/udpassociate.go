package spec

import "context"

// UDPAssociateSocketFunc opens the second socket SOCKS5 UDP ASSOCIATE needs:
// handshake arrives on TCP, but payload needs a separate UDP socket whose
// address is reported back over that TCP. Granted to an inbound's proxy
// server, by core, at construction.
type UDPAssociateSocketFunc func(ctx context.Context) (Datagram, error)

// UDPAssociateHandler is the opt-in hook for a stream proxy server that binds
// a datagram socket mid-handshake -- SOCKS5 UDP ASSOCIATE. Core discovers it by
// type assertion on the StreamProxyServer; an implementation should assert it
// at compile time, so a change here breaks the build rather than UDP.
//
// AcceptStream cannot return a Datagram, so a server signals the handoff by
// returning ErrUDPAssociationRequested. Core then grants the socket function
// once, at construction, through SetAssociateSocket, and calls Associate on
// the same control stream. Associate must open the socket, reply and return
// rather than block for the session's lifetime; core owns the session from
// there, and ends it when the control stream closes.
type UDPAssociateHandler interface {
	SetAssociateSocket(socket UDPAssociateSocketFunc)
	Associate(ctx context.Context, controlStream Stream, metadata Metadata) (Datagram, error)
}
