package socks5

import (
	"errors"
	"fmt"
	"net"
	"sync"

	"github.com/The-Apranik/Apranik/shared/socksaddr"
	"github.com/The-Apranik/Apranik/spec"
)

const maxDatagram = 65507

// association is the RFC 1928 UDP request header on top of a bound socket: below
// it addresses are the client's, above it they are the real destination.
//
// Safe for one reader and many writers, which is what a relay needs.
type association struct {
	socket     spec.Datagram
	readBuffer []byte

	// Reused for every reply. Guarded by writeMutex, which also keeps two
	// replies from interleaving in it.
	writeMutex  sync.Mutex
	writeBuffer []byte

	mu            sync.Mutex
	clientAddress spec.Destination
	clientKnown   bool
}

// declared is the address the client said it would send from. Most clients send
// 0.0.0.0:0, in which case the first packet teaches us instead, and everything
// after it is filtered against that.
func newAssociation(socket spec.Datagram, declared spec.Destination) *association {
	session := &association{socket: socket, readBuffer: make([]byte, maxDatagram)}
	if ip, ok := declared.IP(); ok && !ip.IsUnspecified() && declared.Port() != 0 {
		session.clientAddress, session.clientKnown = declared, true
	}
	return session
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

		packet := a.readBuffer[:size]
		if len(packet) < 4 || packet[0] != 0 || packet[1] != 0 {
			continue
		}
		// Reassembly is optional in RFC 1928 and no common client uses it.
		if packet[2] != 0 {
			continue
		}

		destination, addressLength, err := socksaddr.ParseAddress(packet[3:])
		if err != nil {
			continue
		}

		message := packet[3+addressLength:]
		if len(message) > len(payload) {
			return 0, destination, fmt.Errorf("socks5: %d byte payload into a %d byte buffer: %w", len(message), len(payload), spec.ErrMessageTooLong)
		}
		return copy(payload, message), destination, nil
	}
}

func (a *association) WriteTo(payload []byte, source spec.Destination) (int, error) {
	a.mu.Lock()
	client, known := a.clientAddress, a.clientKnown
	a.mu.Unlock()

	if !known {
		return 0, errors.New("socks5: the client has not sent a datagram yet, so there is nowhere to reply")
	}

	a.writeMutex.Lock()
	defer a.writeMutex.Unlock()

	packet, err := socksaddr.AppendAddress(append(a.writeBuffer[:0], 0, 0, 0), source)
	if err != nil {
		return 0, err
	}
	packet = append(packet, payload...)
	a.writeBuffer = packet

	if _, err := a.socket.WriteTo(packet, client); err != nil {
		return 0, err
	}
	return len(payload), nil
}

func (a *association) Close() error { return a.socket.Close() }

func (a *association) LocalAddr() net.Addr { return a.socket.LocalAddr() }

func (a *association) acceptFrom(sender spec.Destination) bool {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !a.clientKnown {
		a.clientAddress, a.clientKnown = sender, true
		return true
	}
	return a.clientAddress == sender
}
