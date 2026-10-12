package muxcool

import (
	"io"
	"time"

	"github.com/The-Apranik/Apranik/spec"
)

type ServerOptions struct {
	// A New past it is refused with an End carrying the error bit. Zero means no limit.
	MaxConcurrentSubConnections int

	// Over the life of the main connection. Zero means no limit.
	MaxTotalSubConnections int

	// Zero is 60 seconds; a negative value never closes.
	IdleTimeout time.Duration

	// A New for another network is refused like a limit. Zero accepts TCP and UDP.
	AcceptedNetworks Networks
}

// Accepted is a sub-connection the peer opened: Stream for TCP, Datagram for UDP.
type Accepted struct {
	Destination spec.Destination
	Stream      spec.Stream
	Datagram    spec.Datagram
}

// Server is safe for concurrent use.
type Server struct {
	connection *mainConnection
}

// NewServer takes ownership of carrier, which Close closes.
//
// The reader waits for a sub-connection's first data to be read, so AcceptSubConnection and a
// Read must follow promptly.
func NewServer(carrier io.ReadWriteCloser, options ServerOptions) *Server {
	return &Server{connection: newMainConnection(carrier, true, options.MaxConcurrentSubConnections, options.MaxTotalSubConnections, options.IdleTimeout, options.AcceptedNetworks)}
}

func (s *Server) AcceptSubConnection() (Accepted, error) {
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

func (s *Server) ActiveSubConnectionsCount() int { return s.connection.activeSubConnectionsCount() }

func (s *Server) SendKeepAlive() error { return s.connection.sendKeepAlive() }

func (s *Server) Done() <-chan struct{} { return s.connection.done }

func (s *Server) Close() error { return s.connection.close() }
