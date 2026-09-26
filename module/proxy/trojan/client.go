package trojan

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/The-Apranik/Apranik/module/proxy/trojan/wire"
	"github.com/The-Apranik/Apranik/spec"
)

// How long the request waits for the client's first bytes to travel with it.
// A server-first protocol (SMTP, SSH) sends nothing, so after this the request
// goes alone. PROVISIONAL: 100 ms, as Xray waits.
const firstPayloadWait = 100 * time.Millisecond

type client struct {
	passwordHash wire.PasswordHash
}

// Nothing goes on the wire here. The request travels with the first payload,
// so a connection never opens with a header of fixed length on its own. There
// is no reply either, so a wrong password surfaces as the server closing the
// stream.
func (c *client) RequestStream(ctx context.Context, carrier spec.Stream, metadata spec.Metadata) (spec.Stream, error) {
	if metadata.Network == spec.NetworkUDP {
		return nil, fmt.Errorf("trojan: %w: UDP goes through RequestDatagramOverStream", spec.ErrUnsupportedNetwork)
	}
	// Encoded now so a destination the format cannot carry fails here.
	request, err := wire.AppendRequest(nil, wire.Request{PasswordHash: c.passwordHash, Command: wire.CommandConnect, Destination: metadata.Destination})
	if err != nil {
		return nil, err
	}
	return newRequestOnFirstWriteStream(carrier, request, firstPayloadWait), nil
}

// Nothing goes on the wire here either: the request rides with the first
// datagram and names its destination, as Xray and sing-box clients do. A UDP
// session creates this only to send a packet, so no timer is needed.
func (c *client) RequestDatagramOverStream(ctx context.Context, carrier spec.Stream, metadata spec.Metadata) (spec.Datagram, error) {
	return newClientStreamDatagram(carrier, c.passwordHash), nil
}

// requestOnFirstWriteStream puts the request in front of the first Write, or
// sends it alone once the wait runs out, whichever comes first. Exactly one of
// them sends it.
type requestOnFirstWriteStream struct {
	spec.Stream

	requestSent atomic.Bool

	// Guards sending the request; everything after it writes unlocked.
	requestMutex  sync.Mutex
	unsentRequest []byte
	waitTimer     *time.Timer
}

func newRequestOnFirstWriteStream(stream spec.Stream, request []byte, wait time.Duration) *requestOnFirstWriteStream {
	s := &requestOnFirstWriteStream{Stream: stream, unsentRequest: request}
	// Held while the timer is stored, so a timer firing at once finds it set.
	s.requestMutex.Lock()
	defer s.requestMutex.Unlock()
	s.waitTimer = time.AfterFunc(wait, func() { s.sendRequestWith(nil) })
	return s
}

func (s *requestOnFirstWriteStream) Write(payload []byte) (int, error) {
	if !s.requestSent.Load() {
		sent, err := s.sendRequestWith(payload)
		if sent {
			if err != nil {
				return 0, err
			}
			return len(payload), nil
		}
	}
	return s.Stream.Write(payload)
}

// Half-closing before any payload still owes the server its request.
func (s *requestOnFirstWriteStream) CloseWrite() error {
	if _, err := s.sendRequestWith(nil); err != nil {
		return err
	}
	return s.Stream.CloseWrite()
}

func (s *requestOnFirstWriteStream) Close() error {
	s.waitTimer.Stop()
	return s.Stream.Close()
}

// Reports whether this call sent the request, payload included.
func (s *requestOnFirstWriteStream) sendRequestWith(payload []byte) (bool, error) {
	s.requestMutex.Lock()
	defer s.requestMutex.Unlock()
	if s.requestSent.Load() {
		return false, nil
	}
	s.waitTimer.Stop()

	message := append(s.unsentRequest, payload...)
	s.unsentRequest = nil
	s.requestSent.Store(true)
	if _, err := s.Stream.Write(message); err != nil {
		return true, fmt.Errorf("trojan: sending request: %w", err)
	}
	return true, nil
}
