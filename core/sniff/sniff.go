// Package sniff reads the first bytes a client sends on a stream and asks
// each configured sniffer what protocol they are, so routing can see a domain
// the client never named. The bytes are only peeked: the stream it returns
// replays them, so the outbound receives exactly what the client sent.
package sniff

import (
	"errors"
	"time"

	"github.com/The-Apranik/Apranik/core/net/peekable"
	"github.com/The-Apranik/Apranik/spec"
)

// Server-first protocols such as SSH and SMTP send nothing until the server
// speaks, so every connection of theirs waits this long. PROVISIONAL.
const DefaultTimeout = 300 * time.Millisecond

// Stream returns a stream that replays the peeked bytes, and nil when no
// sniffer recognised them. A stream that cannot take a read deadline is not
// sniffed, since nothing would bound the wait.
func Stream(clientStream spec.Stream, sniffers []spec.Sniffer, timeout time.Duration) (spec.Stream, *spec.SniffResult) {
	if len(sniffers) == 0 {
		return clientStream, nil
	}
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	if clientStream.SetReadDeadline(time.Now().Add(timeout)) != nil {
		return clientStream, nil
	}

	peekableStream, ok := clientStream.(*peekable.Stream)
	if !ok {
		peekableStream = peekable.NewStream(clientStream)
	}
	result := peekUntilRecognised(peekableStream, sniffers)
	clientStream.SetReadDeadline(time.Time{})

	return peekableStream, result
}

// The sniffers always see every byte already buffered, and the stream waits
// for the client only when they have seen everything and still need more.
func peekUntilRecognised(clientStream *peekable.Stream, sniffers []spec.Sniffer) *spec.SniffResult {
	undecided := sniffers
	sniffedBytes := 0

	for {
		if buffered := clientStream.Buffered(); buffered > sniffedBytes {
			peeked, _ := clientStream.Peek(buffered)
			sniffedBytes = buffered

			var result *spec.SniffResult
			result, undecided = sniffEach(peeked, undecided)
			if result != nil || len(undecided) == 0 {
				return result
			}
		}

		if _, err := clientStream.Peek(sniffedBytes + 1); err != nil && clientStream.Buffered() <= sniffedBytes {
			return nil
		}
	}
}

// Returns the first match, or the sniffers still waiting for more bytes.
func sniffEach(peeked []byte, sniffers []spec.Sniffer) (*spec.SniffResult, []spec.Sniffer) {
	var needMoreData []spec.Sniffer
	for _, sniffer := range sniffers {
		result, err := sniffer.Sniff(peeked)
		switch {
		case err == nil:
			return &result, nil
		case errors.Is(err, spec.ErrSniffNeedMoreData):
			needMoreData = append(needMoreData, sniffer)
		}
	}
	return nil, needMoreData
}
