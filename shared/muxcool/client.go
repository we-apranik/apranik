package muxcool

import (
	"fmt"
	"io"
	"time"

	"github.com/The-Apranik/Apranik/spec"
)

type ClientOptions struct {
	// Zero means no limit. Xray's client uses 8.
	MaxConcurrentSubConnections int

	// Over the life of the main connection. Zero means no limit. Xray's client uses 128.
	MaxTotalSubConnections int

	// Zero is 15 seconds; a negative value never closes.
	IdleTimeout time.Duration
}

// Client is safe for concurrent use.
type Client struct {
	connection *mainConnection
}

// NewClient takes ownership of carrier, which Close closes.
func NewClient(carrier io.ReadWriteCloser, options ClientOptions) *Client {
	return &Client{connection: newMainConnection(carrier, false, options.MaxConcurrentSubConnections, options.MaxTotalSubConnections, options.IdleTimeout, 0)}
}

// OpenStream sends the New frame at once, with no data.
func (c *Client) OpenStream(destination spec.Destination) (spec.Stream, error) {
	sub, err := c.connection.open(NetworkTCP, destination)
	if err != nil {
		return nil, err
	}
	sub.peerKnows.Store(true)
	header := Header{SubConnectionID: sub.id, Status: StatusNew, Network: NetworkTCP, Destination: destination}
	if err := c.connection.writeFrame(header, nil, sub.closedCh, nil); err != nil {
		sub.close()
		return nil, err
	}
	return &stream{sub}, nil
}

type DatagramOptions struct {
	// XUDP names an address in every datagram, so one sub-connection reaches many destinations.
	XUDP bool
}

// OpenDatagram sends nothing: the New frame leaves with the first WriteTo.
func (c *Client) OpenDatagram(destination spec.Destination, options DatagramOptions) (spec.Datagram, error) {
	sub, err := c.connection.open(NetworkUDP, destination)
	if err != nil {
		return nil, err
	}
	sub.addressInEveryDatagram.Store(options.XUDP)
	return &datagram{sub}, nil
}

// CanOpenSubConnection is a hint: OpenStream and OpenDatagram can still return ErrSubConnectionLimit.
func (c *Client) CanOpenSubConnection() bool { return !c.connection.cannotOpenMore() }

func (c *Client) ActiveSubConnectionsCount() int { return c.connection.activeSubConnectionsCount() }

func (c *Client) SendKeepAlive() error { return c.connection.sendKeepAlive() }

// Done is closed once the main connection has ended.
func (c *Client) Done() <-chan struct{} { return c.connection.done }

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
	if c.cannotOpenMoreLocked() {
		return nil, ErrSubConnectionLimit
	}
	sub := newSubConnection(c, uint16(c.nextID), network, destination, false)
	c.nextID++
	c.everOpened++
	c.subs[sub.id] = sub
	c.stopIdleTimerLocked()
	return sub, nil
}
