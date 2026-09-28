// Package inbound accepts connections and hands each one's payload and
// metadata to the traffic handler. It runs the accept loop, bounds every
// handshake with a timeout, asks a protocol that can frame datagrams in a
// stream which payload each request wants, hands a UDP association to the
// handler as a UDP session, and replays a refused stream to the fallback.
package inbound

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/The-Apranik/Apranik/core/component"
	"github.com/The-Apranik/Apranik/core/logging"
	"github.com/The-Apranik/Apranik/core/net/peekable"
	"github.com/The-Apranik/Apranik/core/relay"
	"github.com/The-Apranik/Apranik/core/sniff"
	"github.com/The-Apranik/Apranik/spec"
)

// EMFILE is not fatal; a hot loop on it would make a busy server worse.
const acceptBackoff = 10 * time.Millisecond

// A client that connects and then says nothing would otherwise hold a
// goroutine and a socket open for as long as it liked.
const DefaultHandshakeTimeout = 10 * time.Second

// PROVISIONAL: 10 seconds, as for an outbound's dial.
const fallbackDialTimeout = 10 * time.Second

// PROVISIONAL: a flood of refused connections is one warning per 10 seconds.
const refusedConnectionLogInterval = 10 * time.Second

type Sniffing struct {
	Sniffers []spec.Sniffer

	// Zero means sniff.DefaultTimeout.
	Timeout time.Duration
}

// Fallback is where a stream goes when the proxy refuses it with
// spec.ErrFallbackRequested: every byte the handshake read is replayed there.
type Fallback struct {
	Dialer      spec.StreamDialer
	Destination spec.Destination

	// Zero means relay.DefaultStreamIdleTimeout; negative means no bound.
	IdleTimeout time.Duration
}

type StreamConfig struct {
	Tag         string
	Listener    spec.StreamListener
	ProxyServer spec.StreamProxyServer

	// Nil unless the protocol also frames datagrams in its streams. Then each
	// request is identified first and sent to the matching accept.
	DatagramOverStreamServer spec.DatagramOverStreamProxyServer

	// Nil means a refused stream is closed.
	Fallback *Fallback

	// Nil means this inbound grants no UDP associate socket; a protocol
	// needing one must refuse.
	AssociateSocket spec.UDPAssociateSocketFunc

	// Zero means DefaultHandshakeTimeout; negative disables the bound.
	HandshakeTimeout time.Duration

	// Connections held at once. One accepted beyond it is closed at once,
	// before any handshake: refusing early is cheaper than queueing. Zero
	// means no limit.
	MaxConnections int

	// No sniffers means no sniffing.
	Sniffing Sniffing

	// Nil is allowed: NewStream substitutes a silent logger.
	Logger *logging.Logger
}

func NewStream(config StreamConfig) component.Inbound {
	logger := config.Logger
	if logger == nil {
		logger = logging.Discard()
	}

	handshakeTimeout := config.HandshakeTimeout
	if handshakeTimeout == 0 {
		handshakeTimeout = DefaultHandshakeTimeout
	}

	if config.Fallback != nil && config.Fallback.IdleTimeout == 0 {
		fallback := *config.Fallback
		fallback.IdleTimeout = relay.DefaultStreamIdleTimeout
		config.Fallback = &fallback
	}

	var connectionSlots chan struct{}
	if config.MaxConnections > 0 {
		connectionSlots = make(chan struct{}, config.MaxConnections)
	}

	return &streamInbound{
		connectionSlots:    connectionSlots,
		refusedLogSampler:  logging.NewSampler(refusedConnectionLogInterval),
		tag:                config.Tag,
		listener:           config.Listener,
		proxyServer:        config.ProxyServer,
		datagramServer:     config.DatagramOverStreamServer,
		fallback:           config.Fallback,
		udpAssociateSocket: config.AssociateSocket,
		handshakeTimeout:   handshakeTimeout,
		sniffing:           config.Sniffing,
		logger:             logger.With("inbound", config.Tag),
	}
}

type streamInbound struct {
	tag                string
	listener           spec.StreamListener
	proxyServer        spec.StreamProxyServer
	datagramServer     spec.DatagramOverStreamProxyServer
	fallback           *Fallback
	udpAssociateSocket spec.UDPAssociateSocketFunc
	handshakeTimeout   time.Duration
	sniffing           Sniffing
	logger             *logging.Logger

	// Nil means no limit. Full means the next connection is refused.
	connectionSlots   chan struct{}
	refusedLogSampler *logging.Sampler

	mu             sync.Mutex
	stopAccepting  context.CancelFunc
	acceptLoopDone chan struct{}

	liveConnections sync.WaitGroup
}

func (i *streamInbound) Tag() string { return i.tag }

func (i *streamInbound) Activate(ctx context.Context, handler component.TrafficHandler) error {
	i.mu.Lock()
	defer i.mu.Unlock()

	if i.acceptLoopDone != nil {
		return fmt.Errorf("inbound %q already activated", i.tag)
	}

	// Only when there is a socket to grant: otherwise a proxy handed one that
	// always fails would answer with a general failure instead of X'07'.
	if associateHandler, ok := i.proxyServer.(spec.UDPAssociateHandler); ok && i.udpAssociateSocket != nil {
		associateHandler.SetAssociateSocket(i.udpAssociateSocket)
	}

	ctx, stopAccepting := context.WithCancel(ctx)
	i.stopAccepting = stopAccepting
	i.acceptLoopDone = make(chan struct{})

	go i.acceptLoop(ctx, handler)
	return nil
}

// Closing the listener, not just cancelling, is what unblocks AcceptStream.
func (i *streamInbound) Close() error {
	i.mu.Lock()
	stopAccepting, acceptLoopDone := i.stopAccepting, i.acceptLoopDone
	i.mu.Unlock()

	if stopAccepting != nil {
		stopAccepting()
	}
	err := i.listener.Close()
	if acceptLoopDone != nil {
		<-acceptLoopDone
	}
	i.liveConnections.Wait()
	if i.fallback != nil {
		err = errors.Join(err, i.fallback.Dialer.Close())
	}
	return err
}

// A shutdown is told apart from a failure by ctx, not the error: Close cancels
// before closing the listener. Matching net.ErrClosed would be wrong for a
// gRPC or WebSocket listener.
func (i *streamInbound) acceptLoop(ctx context.Context, handler component.TrafficHandler) {
	defer close(i.acceptLoopDone)

	for {
		stream, err := i.listener.AcceptStream(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			i.logger.Warn("Accept failed", "error", err)

			select {
			case <-ctx.Done():
				return
			case <-time.After(acceptBackoff):
			}
			continue
		}

		if !i.takeConnectionSlot() {
			stream.Close()
			if allowed, alsoRefused := i.refusedLogSampler.Allow(); allowed {
				i.logger.Warn("Connection refused: max_connections reached",
					"max_connections", cap(i.connectionSlots), "also_refused_since_last_warning", alsoRefused)
			}
			continue
		}

		i.liveConnections.Add(1)
		go func() {
			defer i.liveConnections.Done()
			defer i.releaseConnectionSlot()
			i.handleStream(ctx, handler, stream)
		}()
	}
}

func (i *streamInbound) takeConnectionSlot() bool {
	if i.connectionSlots == nil {
		return true
	}
	select {
	case i.connectionSlots <- struct{}{}:
		return true
	default:
		return false
	}
}

func (i *streamInbound) releaseConnectionSlot() {
	if i.connectionSlots != nil {
		<-i.connectionSlots
	}
}

// Covers the handshake only: once a payload carrier comes back, a long-lived
// tunnel is the normal case rather than a stall.
func (i *streamInbound) handshakeContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if i.handshakeTimeout < 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, i.handshakeTimeout)
}

// Recording only when something may need it: identifying peeks, and a
// fallback replays the whole handshake.
func (i *streamInbound) handleStream(ctx context.Context, handler component.TrafficHandler, stream spec.Stream) {
	carrier := stream
	var recording *peekable.Stream
	if i.datagramServer != nil || i.fallback != nil {
		recording = peekable.NewRecordingStream(stream)
		carrier = recording
	}

	handshakeCtx, endHandshake := i.handshakeContext(ctx)
	defer endHandshake()

	if i.datagramServer != nil {
		isDatagram, err := i.datagramServer.IsDatagramOverStream(handshakeCtx, recording)
		if err != nil {
			stream.Close()
			i.logger.Warn("Handshake failed", "error", err)
			return
		}
		if isDatagram {
			datagram, metadata, err := i.datagramServer.AcceptDatagramOverStream(handshakeCtx, recording)
			endHandshake()
			if err != nil {
				i.refuse(ctx, recording, stream, err)
				return
			}
			recording.StopRecording()
			i.handleDatagramOverStream(ctx, handler, datagram, metadata)
			return
		}
	}

	payload, metadata, err := i.proxyServer.AcceptStream(handshakeCtx, carrier)
	endHandshake()
	if err == nil || errors.Is(err, spec.ErrUDPAssociationRequested) {
		stopRecording(recording)
	}
	if errors.Is(err, spec.ErrUDPAssociationRequested) {
		i.handleUDPAssociate(ctx, handler, carrier, metadata)
		return
	}
	if err != nil {
		i.refuse(ctx, recording, stream, err)
		return
	}

	metadata.InboundTag = i.tag
	if len(i.sniffing.Sniffers) > 0 {
		peekablePayload, alreadyPeekable := payload.(*peekable.Stream)
		if !alreadyPeekable {
			peekablePayload = peekable.NewStream(payload)
		}
		payload = peekablePayload
		metadata.Sniffed = sniff.Recognise(peekablePayload, i.sniffing.Sniffers, i.sniffing.Timeout)
		if metadata.Sniffed != nil {
			i.logger.Debug("Sniffed", "protocol", metadata.Sniffed.Protocol, "domain", metadata.Sniffed.Domain)
		}
	}

	if err := handler.HandleStream(ctx, payload, metadata); err != nil {
		i.logger.Warn("Stream ended in error", "error", err)
	}
}

func (i *streamInbound) handleDatagramOverStream(ctx context.Context, handler component.TrafficHandler, datagram spec.Datagram, metadata spec.Metadata) {
	// The stream is the session: closing it, from either end, ends both, so
	// its end of file is the normal way a session finishes.
	metadata.InboundTag = i.tag
	if err := handler.HandleDatagram(ctx, datagram, metadata); err != nil && !errors.Is(err, io.EOF) {
		i.logger.Warn("Datagram session ended in error", "error", err)
	}
	datagram.Close()
}

// A refusal goes to the fallback only when the protocol asked for it, one is
// configured, and every byte read so far was kept. Anything else is closed.
func (i *streamInbound) refuse(ctx context.Context, recording *peekable.Stream, stream spec.Stream, err error) {
	if !errors.Is(err, spec.ErrFallbackRequested) || i.fallback == nil || recording.Rewind() != nil {
		stream.Close()
		i.logger.Warn("Handshake failed", "error", err)
		return
	}
	recording.StopRecording()

	i.logger.Debug("Stream handed to the fallback", "reason", err)
	dialCtx, endDial := context.WithTimeout(ctx, fallbackDialTimeout)
	fallbackStream, dialErr := i.fallback.Dialer.DialStream(dialCtx, i.fallback.Destination)
	endDial()
	if dialErr != nil {
		recording.Close()
		i.logger.Warn("Fallback unreachable", "fallback", i.fallback.Destination, "error", dialErr)
		return
	}

	relayErr := relay.Stream(ctx, recording, fallbackStream, i.fallback.IdleTimeout)
	if relayErr != nil && !errors.Is(relayErr, relay.ErrStreamIdle) {
		i.logger.Debug("Fallback stream ended in error", "error", relayErr)
	}
}

func stopRecording(recording *peekable.Stream) {
	if recording != nil {
		recording.StopRecording()
	}
}

func (i *streamInbound) handleUDPAssociate(ctx context.Context, handler component.TrafficHandler, controlStream spec.Stream, metadata spec.Metadata) {
	associateHandler, ok := i.proxyServer.(spec.UDPAssociateHandler)
	if !ok {
		controlStream.Close()
		i.logger.Error("Proxy reported a UDP session but does not implement UDPAssociateHandler")
		return
	}

	handshakeCtx, endHandshake := i.handshakeContext(ctx)
	session, err := associateHandler.Associate(handshakeCtx, controlStream, metadata)
	endHandshake()
	if err != nil {
		controlStream.Close()
		i.logger.Warn("Datagram negotiation failed", "error", err)
		return
	}

	// The control stream's lifetime bounds the session. spec has no way to say
	// this, so it is this package's own default.
	go func() {
		io.Copy(io.Discard, controlStream)
		session.Close()
	}()

	metadata.InboundTag = i.tag
	if err := handler.HandleDatagram(ctx, session, metadata); err != nil {
		i.logger.Warn("Datagram session ended in error", "error", err)
	}
	controlStream.Close()
}
