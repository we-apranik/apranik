// Package sniff reads the first bytes a client sends on a stream and asks
// each configured sniffer what protocol they are, so routing can see a domain
// the client never named. The bytes are only peeked: the stream it returns
// replays them, so the outbound receives exactly what the client sent.
package sniff

import (
	"errors"
	"time"

	"github.com/The-Apranik/Apranik/spec"
)

// Server-first protocols such as SSH and SMTP send nothing until the server
// speaks, so every connection of theirs waits this long. PROVISIONAL.
const DefaultTimeout = 300 * time.Millisecond

// Large enough for a post-quantum TLS ClientHello split across records.
const maxPeekedBytes = 16 << 10

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

	peeked, result := peekUntilRecognised(clientStream, sniffers)
	clientStream.SetReadDeadline(time.Time{})

	return &replayingStream{Stream: clientStream, unread: peeked}, result
}

func peekUntilRecognised(clientStream spec.Stream, sniffers []spec.Sniffer) ([]byte, *spec.SniffResult) {
	buffer := make([]byte, maxPeekedBytes)
	peekedSize := 0
	undecided := sniffers

	for peekedSize < len(buffer) {
		readSize, err := clientStream.Read(buffer[peekedSize:])
		peekedSize += readSize

		if readSize > 0 {
			var result *spec.SniffResult
			result, undecided = sniffEach(buffer[:peekedSize], undecided)
			if result != nil || len(undecided) == 0 {
				return buffer[:peekedSize], result
			}
		}
		if err != nil {
			break
		}
	}
	return buffer[:peekedSize], nil
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

type replayingStream struct {
	spec.Stream
	unread []byte
}

func (s *replayingStream) Read(buffer []byte) (int, error) {
	if len(s.unread) > 0 {
		copied := copy(buffer, s.unread)
		s.unread = s.unread[copied:]
		return copied, nil
	}
	return s.Stream.Read(buffer)
}
