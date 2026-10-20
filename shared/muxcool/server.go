package muxcool

import (
	"io"
	"time"

	"github.com/we-apranik/apranik/spec"
)

type ServerOptions struct {
	MaxConcurrentSubConnections int

	MaxTotalSubConnections int

	// Zero is 60 seconds; a negative value never closes.
	IdleTimeout time.Duration

	AcceptedNetworks Networks
}

type AcceptedSubConnection struct {
	Destination spec.Destination
	Stream      spec.Stream
	Datagram    spec.Datagram

	GlobalID [8]byte
}

// Server is safe for concurrent use.
type Server struct {
	connection *mainConnection
}

func NewServer(carrier io.ReadWriteCloser, options ServerOptions) *Server {
	return &Server{connection: newMainConnection(carrier, true, options.MaxConcurrentSubConnections, options.MaxTotalSubConnections, options.IdleTimeout, options.AcceptedNetworks)}
}

func (s *Server) AcceptSubConnection() (AcceptedSubConnection, error) {
	c := s.connection
	for {
		c.mutex.Lock()
		if c.closed {
			cause := c.closeCause
			c.mutex.Unlock()
			return AcceptedSubConnection{}, closedError(cause)
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

func acceptedFrom(sub *subConnection) AcceptedSubConnection {
	accepted := AcceptedSubConnection{Destination: sub.destination, GlobalID: sub.globalID}
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
