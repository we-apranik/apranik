// Package inbound accepts connections and hands each one's payload and
// metadata to the traffic handler. It runs the accept loop, runs each
// connection's handshakes in its own goroutine (every layer bounds its own
// with its handshake_timeout), asks a protocol that can frame datagrams in a
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

	"github.com/we-apranik/apranik/core/component"
	"github.com/we-apranik/apranik/core/logging"
	"github.com/we-apranik/apranik/core/net/peekable"
	"github.com/we-apranik/apranik/core/relay"
	"github.com/we-apranik/apranik/core/stats"
	"github.com/we-apranik/apranik/spec"
)

const acceptBackoff = 10 * time.Millisecond

// PROVISIONAL: 10 seconds, as for an outbound's dial.
const fallbackDialTimeout = 10 * time.Second

// PROVISIONAL: a flood of refused connections is one warning per 10 seconds.
const refusedConnectionLogInterval = 10 * time.Second

type Fallback struct {
	Dialer      spec.StreamDialer
	Destination spec.Destination

	IdleTimeout time.Duration
}

type StreamOverlay struct {
	Name   string
	Server spec.StreamOverlayServer
}

type StreamConfig struct {
	Tag string

	Listener spec.StreamListener

	Overlays []StreamOverlay

	ProxyServer spec.StreamProxyServer

	DatagramOverStreamServer spec.DatagramOverStreamProxyServer

	Fallback *Fallback

	AssociateSocket spec.UDPAssociateSocketFunc

	Counters *stats.InboundCounters

	MaxTCPConnections int

	MaxUDPSessions int

	Logger *logging.Logger
}

func NewStream(config StreamConfig) component.Inbound {
	logger := config.Logger
	if logger == nil {
		logger = logging.Discard()
	}

	if config.Fallback != nil && config.Fallback.IdleTimeout == 0 {
		fallback := *config.Fallback
		fallback.IdleTimeout = relay.DefaultStreamIdleTimeout
		config.Fallback = &fallback
	}

	return &streamInbound{
		tcpConnectionSlots:          newSlots(config.MaxTCPConnections),
		udpSessionSlots:             newSlots(config.MaxUDPSessions),
		refusedLogSampler:           logging.NewSampler(refusedConnectionLogInterval),
		refusedUDPSessionLogSampler: logging.NewSampler(refusedConnectionLogInterval),
		tag:                         config.Tag,
		listener:                    config.Listener,
		overlays:                    config.Overlays,
		proxyServer:                 config.ProxyServer,
		datagramServer:              config.DatagramOverStreamServer,
		fallback:                    config.Fallback,
		udpAssociateSocket:          config.AssociateSocket,
		counters:                    config.Counters,
		logger:                      logger.With("inbound", config.Tag),
	}
}

type streamInbound struct {
	tag                string
	listener           spec.StreamListener
	overlays           []StreamOverlay
	proxyServer        spec.StreamProxyServer
	datagramServer     spec.DatagramOverStreamProxyServer
	fallback           *Fallback
	udpAssociateSocket spec.UDPAssociateSocketFunc
	counters           *stats.InboundCounters
	logger             *logging.Logger

	tcpConnectionSlots *slots
	udpSessionSlots    *slots

	refusedLogSampler           *logging.Sampler
	refusedUDPSessionLogSampler *logging.Sampler

	mu             sync.Mutex
	stopAccepting  context.CancelFunc
	acceptLoopDone chan struct{}

	liveConnections sync.WaitGroup
}

func (i *streamInbound) Tag() string { return i.tag }

var _ component.UserManagingInbound = (*streamInbound)(nil)

func (i *streamInbound) UserManager() spec.UserManager {
	if manager, ok := i.proxyServer.(spec.UserManager); ok {
		return manager
	}
	if manager, ok := i.datagramServer.(spec.UserManager); ok {
		return manager
	}
	return nil
}

func (i *streamInbound) Activate(ctx context.Context, handler component.TrafficHandler) error {
	i.mu.Lock()
	defer i.mu.Unlock()

	if i.acceptLoopDone != nil {
		return fmt.Errorf("inbound %q already activated", i.tag)
	}

	// Only when there is a socket to grant: otherwise a proxy handed one that
	// always fails would answer with a general failure instead of X'07'.
	if associateHandler, ok := i.proxyServer.(spec.UDPAssociateHandler); ok && i.udpAssociateSocket != nil {
		associateHandler.SetAssociateSocket(i.associateSocketWithinLimit(i.udpAssociateSocket))
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

		if !i.tcpConnectionSlots.take() {
			stream.Close()
			i.counters.ConnectionRefusedFor(stats.RefusalMaxTCPConnections)
			if allowed, alsoRefused := i.refusedLogSampler.Allow(); allowed {
				i.logger.Warn("Connection refused: max_tcp_connections reached",
					"max_tcp_connections", i.tcpConnectionSlots.capacity(), "also_refused_since_last_warning", alsoRefused)
			}
			continue
		}

		i.liveConnections.Add(1)
		go func() {
			defer i.liveConnections.Done()
			defer i.tcpConnectionSlots.release()
			i.handleStream(ctx, handler, stream)
		}()
	}
}

func (i *streamInbound) wrapServerOverlays(ctx context.Context, stream spec.Stream, accepted spec.TransportMetadata) (spec.Stream, spec.TransportMetadata, error) {
	editableByOverlays := accepted
	for _, overlay := range i.overlays {
		wrapped, err := overlay.Server.WrapServerStream(ctx, stream, &editableByOverlays)
		if err != nil {
			stream.Close()
			return nil, accepted, fmt.Errorf("transport %q: %w", overlay.Name, err)
		}
		stream = wrapped
	}
	if editableByOverlays.SourceEndpoint != accepted.SourceEndpoint {
		i.logger.Debug("Source replaced by a transport overlay",
			"socket_remote_endpoint", accepted.InboundRemoteEndpoint, "source", editableByOverlays.SourceEndpoint)
	}
	accepted.SourceEndpoint = editableByOverlays.SourceEndpoint
	accepted.ProxyProtocol = editableByOverlays.ProxyProtocol
	return stream, accepted, nil
}

func (i *streamInbound) handleStream(ctx context.Context, handler component.TrafficHandler, stream spec.Stream) {
	socketRemoteEndpoint := spec.NewEndpointFromNetAddr(stream.RemoteAddr())
	acceptedSocket := spec.TransportMetadata{
		SourceEndpoint:        socketRemoteEndpoint,
		InboundRemoteEndpoint: socketRemoteEndpoint,
		InboundLocalEndpoint:  spec.NewEndpointFromNetAddr(stream.LocalAddr()),
	}

	stream, transport, err := i.wrapServerOverlays(ctx, stream, acceptedSocket)
	if err != nil {
		i.counters.ConnectionRefusedFor(refusalReasonFor(err, stats.RefusalTransportHandshakeFailed))
		i.logger.Debug("Transport handshake failed", "error", err)
		return
	}

	handshakeStream := stream
	var recording *peekable.Stream
	if i.datagramServer != nil || i.fallback != nil {
		recording = peekable.NewRecordingStream(stream)
		handshakeStream = recording
	}

	if i.datagramServer != nil {
		isDatagram, err := i.datagramServer.IsDatagramOverStream(ctx, recording)
		if err != nil {
			stream.Close()
			i.counters.ConnectionRefusedFor(refusalReasonFor(err, stats.RefusalProxyHandshakeFailed))
			i.logger.Warn("Handshake failed", "error", err)
			return
		}
		if isDatagram {
			datagram, metadata, err := i.datagramServer.AcceptDatagramOverStream(ctx, recording)
			if err != nil {
				i.refuse(ctx, recording, stream, err)
				return
			}
			recording.StopRecording()
			metadata.Transport = transport
			if !i.udpSessionSlots.take() {
				datagram.Close()
				i.refuseUDPSession()
				return
			}
			defer i.udpSessionSlots.release()
			i.handleDatagramOverStream(ctx, handler, datagram, metadata)
			return
		}
	}

	payload, metadata, err := i.proxyServer.AcceptStream(ctx, handshakeStream)
	if err == nil || errors.Is(err, spec.ErrUDPAssociationRequested) {
		stopRecording(recording)
	}
	metadata.Transport = transport
	if errors.Is(err, spec.ErrUDPAssociationRequested) {
		i.handleUDPAssociate(ctx, handler, handshakeStream, metadata)
		return
	}
	if err != nil {
		i.refuse(ctx, recording, stream, err)
		return
	}

	metadata.InboundTag = i.tag
	if err := handler.HandleStream(ctx, payload, metadata); err != nil {
		i.logger.Warn("Stream ended in error", "error", err)
	}
}

func (i *streamInbound) handleDatagramOverStream(ctx context.Context, handler component.TrafficHandler, datagram spec.Datagram, metadata spec.Metadata) {
	metadata.InboundTag = i.tag
	if err := handler.HandleDatagram(ctx, datagram, metadata); err != nil && !errors.Is(err, io.EOF) {
		i.logger.Warn("Datagram session ended in error", "error", err)
	}
	datagram.Close()
}

func (i *streamInbound) refuse(ctx context.Context, recording *peekable.Stream, stream spec.Stream, err error) {
	if errors.Is(err, spec.ErrAuthenticationFailed) {
		i.counters.AuthenticationFailed()
	}
	if !errors.Is(err, spec.ErrFallbackRequested) || i.fallback == nil || recording.Rewind() != nil {
		stream.Close()
		i.counters.ConnectionRefusedFor(refusalReasonFor(err, stats.RefusalProxyHandshakeFailed))
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
		i.counters.ConnectionRefusedFor(stats.RefusalFallbackUnreachable)
		i.logger.Warn("Fallback unreachable", "fallback", i.fallback.Destination, "error", dialErr)
		return
	}

	i.counters.ConnectionSentToFallback()
	relayErr := relay.Stream(ctx, recording, fallbackStream, i.fallback.IdleTimeout, nil, nil)
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

	session, err := associateHandler.Associate(ctx, controlStream, metadata)
	if err != nil {
		controlStream.Close()
		if !errors.Is(err, errUDPSessionLimitReached) {
			i.logger.Warn("Datagram negotiation failed", "error", err)
		}
		return
	}

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
