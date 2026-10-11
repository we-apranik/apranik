package muxcool

import (
	"io"
	"time"

	"github.com/The-Apranik/Apranik/spec"
)

type ServerOptions struct {
	// MaxConcurrentSubConnections is how many sub-connections the peer may have
	// open at once; a New past it is refused with an End carrying the error
	// bit. Zero means no limit.
	MaxConcurrentSubConnections int

	// MaxSubConnectionsEver is how many the peer may open over the life of this
	// main connection. Zero means no limit.
	MaxSubConnectionsEver int

	// IdleTimeout closes a main connection that has had no open sub-connection
	// for this long. Zero is 60 seconds; a negative value never closes it.
	IdleTimeout time.Duration

	// AcceptedNetworks refuses a New for any other network, as the limits do.
	// Zero accepts TCP and UDP.
	AcceptedNetworks Networks
}

// Accepted is a sub-connection the peer opened: Stream for TCP, Datagram for UDP.
type Accepted struct {
	Destination spec.Destination
	Stream      spec.Stream
	Datagram    spec.Datagram
}

// Server is the end of a main connection that accepts the sub-connections the
// peer opens. It is safe for concurrent use.
type Server struct {
	connection *mainConnection
}

// NewServer starts reading carrier at once. The Server owns the carrier from
// here on and closes it with Close.
//
// The reader waits for the first data of a sub-connection to be read, so Accept
// and a Read of what it returns must follow promptly for every sub-connection
// that comes with data in its New frame.
func NewServer(carrier io.ReadWriteCloser, options ServerOptions) *Server {
	return &Server{connection: newMainConnection(carrier, true, options.MaxConcurrentSubConnections, options.MaxSubConnectionsEver, options.IdleTimeout, options.AcceptedNetworks)}
}

// Accept blocks for the next sub-connection the peer opens. It returns ErrClosed
// once the main connection has ended.
func (s *Server) Accept() (Accepted, error) {
	c := s.connection
	for {
		c.mutex.Lock()
		if c.closed {
			cause := c.closeCause
			c.mutex.Unlock()
			return Accepted{}, closedError(cause)
		}
		if len(c.acceptQueue) > 0 {
			sub := c.acceptQueue[0]
			c.acceptQueue[0] = nil
			c.acceptQueue = c.acceptQueue[1:]
			c.mutex.Unlock()
			return acceptedFrom(sub), nil
		}
		c.mutex.Unlock()

		select {
		case <-c.acceptSignal:
		case <-c.done:
		}
	}
}

func acceptedFrom(sub *subConnection) Accepted {
	accepted := Accepted{Destination: sub.destination}
	if sub.network == NetworkTCP {
		accepted.Stream = &stream{sub}
	} else {
		accepted.Datagram = &datagram{sub}
	}
	return accepted
}

func (s *Server) Active() int { return s.connection.active() }

func (s *Server) SendKeepAlive() error { return s.connection.sendKeepAlive() }

func (s *Server) Done() <-chan struct{} { return s.connection.done }

func (s *Server) Close() error { return s.connection.close() }
