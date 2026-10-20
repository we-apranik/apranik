package muxcool

import (
	"errors"
	"fmt"
	"io"

	"github.com/we-apranik/apranik/spec"
)

// Client is safe for concurrent use.
type Client struct {
	connection *mainConnection
}

func NewClient(carrier io.ReadWriteCloser) *Client {
	return &Client{connection: newMainConnection(carrier, false, 0, 0, -1, 0)}
}

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
	XUDP bool

	GlobalID [8]byte
}

func (c *Client) OpenDatagram(destination spec.Destination, options DatagramOptions) (spec.Datagram, error) {
	if options.GlobalID != [8]byte{} && !options.XUDP {
		return nil, errors.New("muxcool: a Global ID belongs to XUDP; set DatagramOptions.XUDP")
	}
	sub, err := c.connection.open(NetworkUDP, destination)
	if err != nil {
		return nil, err
	}
	sub.addressInEveryDatagram.Store(options.XUDP)
	sub.globalID = options.GlobalID
	return &datagram{sub}, nil
}

func (c *Client) ActiveSubConnectionsCount() int { return c.connection.activeSubConnectionsCount() }

func (c *Client) SendKeepAlive() error { return c.connection.sendKeepAlive() }

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
	if c.nextID >= idCount {
		return nil, errIDsUsedUp
	}
	sub := newSubConnection(c, uint16(c.nextID), network, destination, false)
	c.nextID++
	c.everOpened++
	c.subs[sub.id] = sub
	c.stopIdleTimerLocked()
	return sub, nil
}
