package muxcool

import (
	"fmt"
	"io"
	"time"

	"github.com/The-Apranik/Apranik/spec"
)

type ClientOptions struct {
	// MaxConcurrentSubConnections is how many sub-connections may be open at once
	// on this main connection; zero means no limit. Xray's client uses 8.
	MaxConcurrentSubConnections int

	// MaxSubConnectionsEver is how many may be opened over the life of this main
	// connection, even one after another; zero means no limit. Xray's client uses 128.
	MaxSubConnectionsEver int

	// IdleTimeout closes a main connection that has had no open sub-connection
	// for this long. Zero is 15 seconds; a negative value never closes it.
	IdleTimeout time.Duration
}

// Client is the end of a main connection that opens sub-connections. It is safe
// for concurrent use.
type Client struct {
	connection *mainConnection
}

// NewClient starts reading carrier at once. The Client owns the carrier from
// here on and closes it with Close.
func NewClient(carrier io.ReadWriteCloser, options ClientOptions) *Client {
	return &Client{connection: newMainConnection(carrier, false, options.MaxConcurrentSubConnections, options.MaxSubConnectionsEver, options.IdleTimeout, 0)}
}

// OpenStream opens a TCP sub-connection to destination: the New frame goes out
// at once, with no data.
func (c *Client) OpenStream(destination spec.Destination) (spec.Stream, error) {
	sub, err := c.connection.open(NetworkTCP, destination)
	if err != nil {
		return nil, err
	}
	sub.peerKnows.Store(true)
	header := Header{SessionID: sub.id, Status: StatusNew, Network: NetworkTCP, Destination: destination}
	if err := c.connection.writeFrame(header, nil, sub.closedCh, nil); err != nil {
		sub.close()
		return nil, err
	}
	return &stream{sub}, nil
}

// OpenDatagram opens a UDP sub-connection bound to destination. Nothing is sent
// yet: the New frame leaves with the first WriteTo and carries that datagram.
func (c *Client) OpenDatagram(destination spec.Destination) (spec.Datagram, error) {
	sub, err := c.connection.open(NetworkUDP, destination)
	if err != nil {
		return nil, err
	}
	return &datagram{sub}, nil
}

// Full says that no sub-connection can be opened here any more: a limit was
// reached, the 65535 IDs are used, or the main connection is closed. The caller
// uses another main connection.
func (c *Client) Full() bool { return c.connection.full() }

// Active is how many sub-connections are open now, for a caller with an idle
// policy of its own.
func (c *Client) Active() int { return c.connection.active() }

// SendKeepAlive sends a KeepAlive frame, which keeps the peer from closing the
// main connection as idle.
func (c *Client) SendKeepAlive() error { return c.connection.sendKeepAlive() }

// Done is closed once the main connection has ended, for whatever reason.
func (c *Client) Done() <-chan struct{} { return c.connection.done }

// Close ends every sub-connection and closes the carrier.
func (c *Client) Close() error { return c.connection.close() }

func (c *mainConnection) open(network Network, destination spec.Destination) (*subConnection, error) {
	if !destination.IsValid() {
		return nil, fmt.Errorf("muxcool: cannot open a sub-connection without a destination")
	}
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if c.closed {
		return nil, closedError(c.closeCause)
	}
	if c.fullLocked() {
		return nil, ErrFull
	}
	sub := newSubConnection(c, uint16(c.nextID), network, destination, false)
	c.nextID++
	c.everOpened++
	c.subs[sub.id] = sub
	c.stopIdleTimerLocked()
	return sub, nil
}
