// Package dnsstream carries DNS messages over a stream, as DNS over TCP (RFC
// 7766) and DNS over TLS (RFC 7858) do: each message behind a two-byte length,
// one connection reused for many queries, queries pipelined on it and matched
// to their responses by message ID. The dns server modules tcp and dot share
// it; the stream, encrypted or not, comes from the dialer they are given.
package dnsstream

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/we-apranik/apranik/spec"
)

// IdleTimeout: a connection nothing has been read from for this long is
// closed, as RFC 7766 section 6.2.3 asks of clients. PROVISIONAL.
const IdleTimeout = 30 * time.Second

const (
	headerLength     = 12
	maxMessageLength = 1<<16 - 1
)

var (
	ErrClosed = errors.New("dnsstream: client is closed")

	errMessageTooShort = errors.New("dnsstream: a DNS message is at least 12 bytes")
	errMessageTooLong  = errors.New("dnsstream: a DNS message over a stream is at most 65535 bytes")
	errConnectionLost  = errors.New("dnsstream: connection closed before the answer arrived")
	errTooManyPending  = errors.New("dnsstream: every message ID on the connection is in use")
)

// Client is safe for concurrent use.
type Client struct {
	dialer  spec.StreamDialer
	server  spec.Destination
	timeout time.Duration

	mu         sync.Mutex
	connection *connection
	dialing    chan struct{} // closed when the dial in progress ends; nil when none is
	closed     bool
}

// NewClient queries server through dialer, each query bounded by timeout,
// dial included.
func NewClient(dialer spec.StreamDialer, server spec.Destination, timeout time.Duration) *Client {
	return &Client{dialer: dialer, server: server, timeout: timeout}
}

// Query sends one message and returns its response, with the caller's
// message ID. A connection the server closed while it sat idle is replaced
// once, transparently, since servers may close idle connections at any time.
func (c *Client) Query(ctx context.Context, queryMessage []byte) ([]byte, error) {
	if len(queryMessage) < headerLength {
		return nil, errMessageTooShort
	}
	if len(queryMessage) > maxMessageLength {
		return nil, errMessageTooLong
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	for attempt := 0; ; attempt++ {
		active, freshlyDialed, err := c.activeConnection(ctx)
		if err != nil {
			return nil, err
		}
		response, err := active.exchange(ctx, queryMessage)
		if errors.Is(err, errConnectionLost) && !freshlyDialed && attempt == 0 && ctx.Err() == nil {
			continue
		}
		return response, err
	}
}

// activeConnection returns the open connection, dialing one when there is
// none. One dial at a time; others wait for it or their context.
func (c *Client) activeConnection(ctx context.Context) (*connection, bool, error) {
	for {
		c.mu.Lock()
		if c.closed {
			c.mu.Unlock()
			return nil, false, ErrClosed
		}
		if c.connection != nil && !c.connection.isDead() {
			active := c.connection
			c.mu.Unlock()
			return active, false, nil
		}
		if c.dialing != nil {
			dialing := c.dialing
			c.mu.Unlock()
			select {
			case <-dialing:
				continue
			case <-ctx.Done():
				return nil, false, ctx.Err()
			}
		}
		dialing := make(chan struct{})
		c.dialing = dialing
		c.mu.Unlock()

		stream, err := c.dialer.DialStream(ctx, c.server)

		c.mu.Lock()
		c.dialing = nil
		close(dialing)
		if err != nil {
			c.mu.Unlock()
			return nil, false, fmt.Errorf("dnsstream: dial %s: %w", c.server, err)
		}
		if c.closed {
			c.mu.Unlock()
			stream.Close()
			return nil, false, ErrClosed
		}
		active := newConnection(stream)
		c.connection = active
		c.mu.Unlock()
		return active, true, nil
	}
}

// Close closes the connection; queries waiting on it fail.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	if c.connection != nil {
		c.connection.fail(ErrClosed)
		c.connection = nil
	}
	return nil
}

// connection is one stream and the queries pipelined on it. Its own message
// IDs replace the callers' on the wire, so two callers that picked the same ID
// never collide; the caller's is put back in the response.
type connection struct {
	stream spec.Stream

	writeMu sync.Mutex

	mu      sync.Mutex
	pending map[uint16]chan []byte
	nextID  uint16
	dead    error
	done    chan struct{}
}

func newConnection(stream spec.Stream) *connection {
	active := &connection{stream: stream, pending: make(map[uint16]chan []byte), done: make(chan struct{})}
	go active.readResponses()
	return active
}

func (c *connection) isDead() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.dead != nil
}

func (c *connection) exchange(ctx context.Context, queryMessage []byte) ([]byte, error) {
	wireID, responses, err := c.register()
	if err != nil {
		return nil, err
	}
	defer c.unregister(wireID)

	framed := make([]byte, 2+len(queryMessage))
	binary.BigEndian.PutUint16(framed, uint16(len(queryMessage)))
	copy(framed[2:], queryMessage)
	binary.BigEndian.PutUint16(framed[2:], wireID)

	if err := c.write(ctx, framed); err != nil {
		c.fail(err)
		return nil, errConnectionLost
	}

	select {
	case response := <-responses:
		copy(response, queryMessage[:2])
		return response, nil
	case <-c.done:
		return nil, errConnectionLost
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (c *connection) write(ctx context.Context, framed []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if deadline, hasDeadline := ctx.Deadline(); hasDeadline {
		c.stream.SetWriteDeadline(deadline)
	}
	_, err := c.stream.Write(framed)
	return err
}

func (c *connection) register() (uint16, chan []byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.dead != nil {
		return 0, nil, errConnectionLost
	}
	for range 1 << 16 {
		wireID := c.nextID
		c.nextID++
		if _, inUse := c.pending[wireID]; !inUse {
			responses := make(chan []byte, 1)
			c.pending[wireID] = responses
			return wireID, responses, nil
		}
	}
	return 0, nil, errTooManyPending
}

func (c *connection) unregister(wireID uint16) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.pending, wireID)
}

// fail ends the connection once; every query waiting on it learns at once.
func (c *connection) fail(reason error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.dead != nil {
		return
	}
	c.dead = reason
	close(c.done)
	c.stream.Close()
}

// readResponses runs for the connection's life and ends when the stream does:
// on a read error, on a malformed frame, or after IdleTimeout of silence.
func (c *connection) readResponses() {
	var lengthPrefix [2]byte
	for {
		c.stream.SetReadDeadline(time.Now().Add(IdleTimeout))
		if _, err := io.ReadFull(c.stream, lengthPrefix[:]); err != nil {
			c.fail(err)
			return
		}
		length := int(binary.BigEndian.Uint16(lengthPrefix[:]))
		if length < headerLength {
			c.fail(errMessageTooShort)
			return
		}
		response := make([]byte, length)
		if _, err := io.ReadFull(c.stream, response); err != nil {
			c.fail(err)
			return
		}

		wireID := binary.BigEndian.Uint16(response)
		c.mu.Lock()
		responses, waiting := c.pending[wireID]
		if waiting {
			delete(c.pending, wireID)
		}
		c.mu.Unlock()
		// A response nobody waits for any more (its query timed out) is dropped.
		if waiting {
			responses <- response
		}
	}
}
