package informant

import (
	"context"
	"net/netip"
	"time"

	"github.com/we-apranik/apranik/core/logging"
	"github.com/we-apranik/apranik/core/net/peekable"
	"github.com/we-apranik/apranik/core/sniff"
	"github.com/we-apranik/apranik/spec"
)

// Stream answers the router about one TCP connection. The
// stream is wrapped for peeking only when a rule sniffs, so a connection
// nobody sniffs is relayed untouched; one that was sniffed must be relayed
// from StreamToRelay, which replays the peeked bytes.
type Stream struct {
	metadata     spec.Metadata
	clientStream spec.Stream

	peekableStream *peekable.Stream
	sniffed        *spec.SniffResult
	sniffDone      bool

	resolver   DomainResolver
	logger     *logging.Logger
	resolution destinationResolution
}

// NewStream makes the informant for one TCP connection.
func NewStream(metadata spec.Metadata, clientStream spec.Stream, resolver DomainResolver, logger *logging.Logger) *Stream {
	return &Stream{metadata: metadata, clientStream: clientStream, resolver: resolver, logger: logger}
}

func (i *Stream) Resolve(ctx context.Context, serverTag string) []netip.Addr {
	return i.resolution.resolve(ctx, i.resolver, i.logger, i.metadata.Destination, serverTag)
}

func (i *Stream) Metadata() spec.Metadata { return i.metadata }

func (i *Stream) Sniff(_ context.Context, sniffers []spec.Sniffer, timeout time.Duration) *spec.SniffResult {
	if i.sniffDone {
		return i.sniffed
	}
	i.sniffDone = true

	if i.peekableStream == nil {
		alreadyPeekable, isPeekable := i.clientStream.(*peekable.Stream)
		if !isPeekable {
			alreadyPeekable = peekable.NewStream(i.clientStream)
		}
		i.peekableStream = alreadyPeekable
	}
	i.sniffed = sniff.Recognise(i.peekableStream, sniffers, timeout)
	i.metadata.Sniffed = i.sniffed
	return i.sniffed
}

func (i *Stream) StreamToRelay() spec.Stream {
	if i.peekableStream != nil {
		return i.peekableStream
	}
	return i.clientStream
}

// UDPSession answers for one destination of a UDP session.
// Nothing is sniffed until UDP sniffing exists (open question Q2).
type UDPSession struct {
	metadata spec.Metadata

	resolver   DomainResolver
	logger     *logging.Logger
	resolution destinationResolution
}

// NewUDPSession makes the informant for one destination of a UDP session.
func NewUDPSession(metadata spec.Metadata, resolver DomainResolver, logger *logging.Logger) *UDPSession {
	return &UDPSession{metadata: metadata, resolver: resolver, logger: logger}
}

func (i *UDPSession) Metadata() spec.Metadata { return i.metadata }

func (*UDPSession) Sniff(context.Context, []spec.Sniffer, time.Duration) *spec.SniffResult {
	return nil
}

func (i *UDPSession) Resolve(ctx context.Context, serverTag string) []netip.Addr {
	return i.resolution.resolve(ctx, i.resolver, i.logger, i.metadata.Destination, serverTag)
}
