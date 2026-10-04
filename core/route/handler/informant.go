package handler

import (
	"context"
	"net/netip"
	"time"

	"github.com/The-Apranik/Apranik/core/logging"
	"github.com/The-Apranik/Apranik/core/net/peekable"
	"github.com/The-Apranik/Apranik/core/sniff"
	"github.com/The-Apranik/Apranik/spec"
)

// streamTrafficInformant answers the router about one TCP connection. The
// stream is wrapped for peeking only when a rule sniffs, so a connection
// nobody sniffs is relayed untouched; one that was sniffed must be relayed
// from streamToRelay, which replays the peeked bytes.
type streamTrafficInformant struct {
	metadata     spec.Metadata
	clientStream spec.Stream

	peekableStream *peekable.Stream
	sniffed        *spec.SniffResult
	sniffDone      bool

	resolver   DomainResolver
	logger     *logging.Logger
	resolution destinationResolution
}

func newStreamTrafficInformant(metadata spec.Metadata, clientStream spec.Stream, resolver DomainResolver, logger *logging.Logger) *streamTrafficInformant {
	return &streamTrafficInformant{metadata: metadata, clientStream: clientStream, resolver: resolver, logger: logger}
}

func (i *streamTrafficInformant) Resolve(ctx context.Context, serverTag string) []netip.Addr {
	return i.resolution.resolve(ctx, i.resolver, i.logger, i.metadata.Destination, serverTag)
}

func (i *streamTrafficInformant) Metadata() spec.Metadata { return i.metadata }

func (i *streamTrafficInformant) Sniff(_ context.Context, sniffers []spec.Sniffer, timeout time.Duration) *spec.SniffResult {
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

func (i *streamTrafficInformant) streamToRelay() spec.Stream {
	if i.peekableStream != nil {
		return i.peekableStream
	}
	return i.clientStream
}

// udpSessionTrafficInformant answers for one destination of a UDP session.
// Nothing is sniffed until UDP sniffing exists (open question Q2).
type udpSessionTrafficInformant struct {
	metadata spec.Metadata

	resolver   DomainResolver
	logger     *logging.Logger
	resolution destinationResolution
}

func (i *udpSessionTrafficInformant) Metadata() spec.Metadata { return i.metadata }

func (*udpSessionTrafficInformant) Sniff(context.Context, []spec.Sniffer, time.Duration) *spec.SniffResult {
	return nil
}

func (i *udpSessionTrafficInformant) Resolve(ctx context.Context, serverTag string) []netip.Addr {
	return i.resolution.resolve(ctx, i.resolver, i.logger, i.metadata.Destination, serverTag)
}
