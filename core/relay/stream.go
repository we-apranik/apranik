// Package relay moves payload between a client and its outbounds, so no module
// has to. Streams are copied both ways with half-close carried across.
// Datagrams go to the client's UDP session through the outbound the caller
// names for each packet, and the session's replies come back.
package relay

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/The-Apranik/Apranik/spec"
)

// PROVISIONAL: 5 minutes.
const DefaultStreamIdleTimeout = 5 * time.Minute

var ErrStreamIdle = errors.New("relay: stream idle timeout")

// Cancelling ctx closes both streams, the only thing that unblocks a blocked
// Read. So does silence in both directions for idleTimeout; zero or negative
// means no idle bound.
func Stream(ctx context.Context, clientSide, remoteSide spec.Stream, idleTimeout time.Duration) error {
	defer clientSide.Close()
	defer remoteSide.Close()

	closeBoth := func() {
		clientSide.Close()
		remoteSide.Close()
	}

	if ctx.Done() != nil {
		finished := make(chan struct{})
		defer close(finished)

		go func() {
			select {
			case <-ctx.Done():
				closeBoth()
			case <-finished:
			}
		}()
	}

	var clientSource, remoteSource io.Reader = clientSide, remoteSide
	var idled atomic.Bool
	if idleTimeout > 0 {
		activity := &streamActivity{}
		activity.markActive()
		clientSource = activityReader{source: clientSide, activity: activity}
		remoteSource = activityReader{source: remoteSide, activity: activity}

		var idleTimer *time.Timer
		idleTimer = time.AfterFunc(idleTimeout, func() {
			if silentFor := activity.silentFor(); silentFor < idleTimeout {
				idleTimer.Reset(idleTimeout - silentFor)
				return
			}
			idled.Store(true)
			closeBoth()
		})
		defer idleTimer.Stop()
	}

	copyErrors := make(chan error, 2)
	go func() { copyErrors <- forwardStream(remoteSide, clientSource) }()
	go func() { copyErrors <- forwardStream(clientSide, remoteSource) }()

	var firstError error
	for range 2 {
		if err := <-copyErrors; err != nil && firstError == nil {
			firstError = err
		}
	}

	switch {
	case idled.Load():
		return ErrStreamIdle
	case firstError != nil && ctx.Err() != nil:
		return ctx.Err()
	default:
		return firstError
	}
}

// A CloseWrite failure never fails the relay: ErrNoHalfClose means the Close in
// Stream ends the connection instead, and any other failure means the carrier
// is already gone.
func forwardStream(destination spec.Stream, source io.Reader) error {
	copyBuffer := streamCopyBuffers.Get().(*[]byte)
	defer streamCopyBuffers.Put(copyBuffer)

	_, err := io.CopyBuffer(destination, source, *copyBuffer)
	_ = destination.CloseWrite()
	return err
}

// Every connection copies in both directions, and io.Copy would allocate a
// fresh buffer for each; reusing them keeps short connections off the heap.
// The size is io.Copy's own. Pointers, so Put does not allocate.
const streamCopyBufferSize = 32 << 10

var streamCopyBuffers = sync.Pool{New: func() any {
	buffer := make([]byte, streamCopyBufferSize)
	return &buffer
}}

type streamActivity struct{ lastActiveUnixNano atomic.Int64 }

func (a *streamActivity) markActive() { a.lastActiveUnixNano.Store(time.Now().UnixNano()) }

func (a *streamActivity) silentFor() time.Duration {
	return time.Since(time.Unix(0, a.lastActiveUnixNano.Load()))
}

type activityReader struct {
	source   io.Reader
	activity *streamActivity
}

func (r activityReader) Read(buffer []byte) (int, error) {
	readSize, err := r.source.Read(buffer)
	if readSize > 0 {
		r.activity.markActive()
	}
	return readSize, err
}
