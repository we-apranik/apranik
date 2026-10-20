// Package sniff recognises the protocol at the start of a client's stream, so routing can see a domain the client never named.
package sniff

import (
	"errors"
	"time"

	"github.com/we-apranik/apranik/core/net/peekable"
	"github.com/we-apranik/apranik/spec"
)

// PROVISIONAL.
const DefaultTimeout = 300 * time.Millisecond

// Nil when no sniffer recognised the bytes, or when the stream takes no read deadline, since nothing
// would then bound the wait.
func Recognise(clientStream *peekable.Stream, sniffers []spec.Sniffer, timeout time.Duration) *spec.SniffResult {
	if len(sniffers) == 0 {
		return nil
	}
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	if clientStream.SetReadDeadline(time.Now().Add(timeout)) != nil {
		return nil
	}
	defer clientStream.SetReadDeadline(time.Time{})

	undecided := sniffers
	seenBytes := 0
	for {
		prefix, err := peekBeyond(clientStream, seenBytes)
		if err != nil {
			return nil
		}
		seenBytes = len(prefix)

		result, needMoreData := firstRecognised(prefix, undecided)
		if result != nil || len(needMoreData) == 0 {
			return result
		}
		undecided = needMoreData
	}
}

func peekBeyond(clientStream *peekable.Stream, seenBytes int) ([]byte, error) {
	if clientStream.Buffered() <= seenBytes {
		if _, err := clientStream.Peek(seenBytes + 1); err != nil {
			return nil, err
		}
	}
	return clientStream.Peek(clientStream.Buffered())
}

func firstRecognised(prefix []byte, sniffers []spec.Sniffer) (*spec.SniffResult, []spec.Sniffer) {
	var needMoreData []spec.Sniffer
	for _, sniffer := range sniffers {
		result, err := sniffer.Sniff(prefix)
		switch {
		case err == nil:
			return &result, nil
		case errors.Is(err, spec.ErrSniffNeedMoreData):
			needMoreData = append(needMoreData, sniffer)
		}
	}
	return nil, needMoreData
}
