package vless

import (
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/we-apranik/apranik/module/proxy/vless/wire"
	"github.com/we-apranik/apranik/shared/delayedrequest"
	"github.com/we-apranik/apranik/shared/muxcool"
	"github.com/we-apranik/apranik/spec"
)

func (c *client) requestXUDP(underlyingStream spec.Stream, globalID [8]byte) (spec.Datagram, error) {
	request, err := wire.AppendRequestHeader(nil, wire.Request{UUID: c.uuid, Command: wire.CommandMux})
	if err != nil {
		return nil, err
	}
	delayed := delayedrequest.NewStream(underlyingStream, request, c.requestTiming.MaxDelay)
	carrier := &responseHeaderSkippingStream{Stream: delayed}
	return &xudpClientDatagram{
		mux:      muxcool.NewClient(carrier),
		carrier:  carrier,
		globalID: globalID,
		opened:   make(chan struct{}),
		closedCh: make(chan struct{}),
	}, nil
}

// xudpClientDatagram opens its sub-connection at the first WriteTo, because
// the first destination is known only then. ReadFrom waits for it.
//
// Safe for one reader and many writers, which is what a relay needs.
type xudpClientDatagram struct {
	mux      *muxcool.Client
	carrier  spec.Stream
	globalID [8]byte

	openMutex sync.Mutex // guards the opening of sub
	sub       spec.Datagram
	opened    chan struct{} // closed once sub is set

	closeOnce sync.Once
	closedCh  chan struct{}
}

func (d *xudpClientDatagram) WriteTo(payload []byte, destination spec.Destination) (int, error) {
	sub, err := d.openFor(destination)
	if err != nil {
		return 0, err
	}
	return sub.WriteTo(payload, destination)
}

func (d *xudpClientDatagram) openFor(firstDestination spec.Destination) (spec.Datagram, error) {
	d.openMutex.Lock()
	defer d.openMutex.Unlock()
	select {
	case <-d.closedCh:
		return nil, net.ErrClosed
	default:
	}
	if d.sub != nil {
		return d.sub, nil
	}
	sub, err := d.mux.OpenDatagram(firstDestination, muxcool.DatagramOptions{XUDP: true, GlobalID: d.globalID})
	if err != nil {
		return nil, fmt.Errorf("vless: opening the XUDP sub-connection: %w", err)
	}
	d.sub = sub
	close(d.opened)
	return sub, nil
}

func (d *xudpClientDatagram) ReadFrom(payload []byte) (int, spec.Destination, error) {
	select {
	case <-d.opened:
	case <-d.closedCh:
		return 0, spec.Destination{}, net.ErrClosed
	}
	return d.sub.ReadFrom(payload)
}

func (d *xudpClientDatagram) Close() error {
	d.closeOnce.Do(func() {
		close(d.closedCh)
		d.mux.Close()
	})
	return nil
}

func (d *xudpClientDatagram) LocalAddr() net.Addr { return d.carrier.LocalAddr() }

func (s *server) acceptXUDP(underlyingStream spec.Stream) (spec.Datagram, [8]byte, error) {
	carrier := newResponseHeaderFirstStream(underlyingStream)
	underlyingStream.SetReadDeadline(time.Now().Add(s.handshakeTimeout))
	mux := muxcool.NewServer(carrier, muxcool.ServerOptions{
		MaxConcurrentSubConnections: 1,
		MaxTotalSubConnections:      1,
		AcceptedNetworks:            muxcool.NetworksUDP,
	})
	accepted, err := mux.AcceptSubConnection()
	if err != nil {
		mux.Close()
		return nil, [8]byte{}, fmt.Errorf("vless: waiting for the XUDP sub-connection: %w", err)
	}
	underlyingStream.SetReadDeadline(time.Time{})
	return &xudpServerDatagram{sub: accepted.Datagram, mux: mux, carrier: carrier}, accepted.GlobalID, nil
}

// xudpServerDatagram reads each datagram with the destination it is for, and
// writes each reply with the source it came from.
//
// Safe for one reader and many writers, which is what a relay needs.
type xudpServerDatagram struct {
	sub     spec.Datagram
	mux     *muxcool.Server
	carrier spec.Stream

	closeOnce sync.Once
}

// A client may end its sub-connection with the error bit set (Xray does, when it
// cancels one), which is how a session ends, not a failure on this side.
func (d *xudpServerDatagram) ReadFrom(payload []byte) (int, spec.Destination, error) {
	n, from, err := d.sub.ReadFrom(payload)
	if errors.Is(err, muxcool.ErrRemoteReset) {
		return n, from, io.EOF
	}
	return n, from, err
}

func (d *xudpServerDatagram) WriteTo(payload []byte, source spec.Destination) (int, error) {
	return d.sub.WriteTo(payload, source)
}

func (d *xudpServerDatagram) Close() error {
	d.closeOnce.Do(func() {
		d.sub.Close()
		d.mux.Close()
	})
	return nil
}

func (d *xudpServerDatagram) LocalAddr() net.Addr { return d.carrier.LocalAddr() }
