package socks5

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"sync"

	"github.com/we-apranik/apranik/module/proxy/socks5/wire"
	"github.com/we-apranik/apranik/shared/socksaddr"
	"github.com/we-apranik/apranik/spec"
)

// The most the RFC 1928 UDP header can take before the payload: 3 reserved and
// fragment bytes, the address type, a 255 byte domain with its length, a port.
const largestUDPHeader = 3 + 1 + 1 + 255 + 2

// association is the RFC 1928 UDP request header on top of a bound socket: below
// it addresses are the client's, above it they are the real destination.
//
// Safe for one reader and many writers, which is what a relay needs.
type association struct {
	socket     spec.Datagram
	readBuffer []byte

	// Reused for every reply; the mutex also keeps two replies from
	// interleaving in it.
	replyBufferMutex sync.Mutex
	replyBuffer      []byte

	clientAddressMutex sync.Mutex
	clientAddress      spec.Destination
	clientAddressKnown bool
}

// declaredClientAddress is the address the client said it would send from.
// Most clients send 0.0.0.0:0, in which case the first datagram teaches us
// instead, and everything after it is filtered against that.
//
// maxPayload is the largest datagram it takes from the client, header aside; a
// bigger one is dropped.
func newAssociation(socket spec.Datagram, declaredClientAddress spec.Destination, maxPayload int) *association {
	created := &association{socket: socket, readBuffer: make([]byte, maxPayload+largestUDPHeader)}
	if ip, isIP := declaredClientAddress.IP(); isIP && !ip.IsUnspecified() && declaredClientAddress.Port() != 0 {
		created.clientAddress, created.clientAddressKnown = declaredClientAddress, true
	}
	return created
}

func (a *association) ReadFrom(payload []byte) (int, spec.Destination, error) {
	for {
		size, sender, err := a.socket.ReadFrom(a.readBuffer)
		if err != nil {
			return 0, spec.Destination{}, err
		}
		if !a.acceptFrom(sender) {
			continue
		}

		datagram := a.readBuffer[:size]
		if len(datagram) <= len(wire.UDPHeaderStart) || !bytes.HasPrefix(datagram, wire.UDPHeaderStart[:]) {
			// Wrong reserved bytes, or a fragment: reassembly is optional in
			// RFC 1928 and no common client uses it.
			continue
		}

		destination, addressLength, err := socksaddr.ParseAddress(datagram[len(wire.UDPHeaderStart):])
		if err != nil {
			continue
		}

		message := datagram[len(wire.UDPHeaderStart)+addressLength:]
		if len(message) > len(payload) {
			return 0, destination, fmt.Errorf("socks5: %d byte payload into a %d byte buffer: %w", len(message), len(payload), spec.ErrMessageTooLong)
		}
		return copy(payload, message), destination, nil
	}
}

func (a *association) WriteTo(payload []byte, source spec.Destination) (int, error) {
	a.clientAddressMutex.Lock()
	clientAddress, clientAddressKnown := a.clientAddress, a.clientAddressKnown
	a.clientAddressMutex.Unlock()

	if !clientAddressKnown {
		return 0, errors.New("socks5: the client has not sent a datagram yet, so there is nowhere to reply")
	}

	a.replyBufferMutex.Lock()
	defer a.replyBufferMutex.Unlock()

	reply, err := socksaddr.AppendAddress(append(a.replyBuffer[:0], wire.UDPHeaderStart[:]...), source)
	if err != nil {
		return 0, err
	}
	reply = append(reply, payload...)
	a.replyBuffer = reply

	if _, err := a.socket.WriteTo(reply, clientAddress); err != nil {
		return 0, err
	}
	return len(payload), nil
}

func (a *association) Close() error { return a.socket.Close() }

func (a *association) LocalAddr() net.Addr { return a.socket.LocalAddr() }

func (a *association) acceptFrom(sender spec.Destination) bool {
	a.clientAddressMutex.Lock()
	defer a.clientAddressMutex.Unlock()

	if !a.clientAddressKnown {
		a.clientAddress, a.clientAddressKnown = sender, true
		return true
	}
	return a.clientAddress == sender
}
