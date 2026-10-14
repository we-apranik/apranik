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

	"github.com/we-apranik/apranik/core/stats"
	"github.com/we-apranik/apranik/spec"
)

// PROVISIONAL: 5 minutes.
const DefaultStreamIdleTimeout = 5 * time.Minute

var ErrStreamIdle = errors.New("relay: stream idle timeout")

// Cancelling ctx closes both streams, the only thing that unblocks a blocked
// Read. So does silence in both directions for idleTimeout; zero or negative
// means no idle bound.
//
// Every counters entry, and the connection's own bytes, get the bytes read
// from the client as uplink and the bytes read from the remote side as
// downlink, as they pass. Nil entries are skipped; nothing to count means
// nothing is counted and nothing is wrapped.
func Stream(ctx context.Context, clientSide, remoteSide spec.Stream, idleTimeout time.Duration, counters []*stats.Counters, connectionBytes *stats.ConversationBytes) error {
	defer clientSide.Close()
	defer remoteSide.Close()

	closeBoth := func() {
		clientSide.Close()
		remoteSide.Close()
	}

	stopClosingOnCancel := context.AfterFunc(ctx, closeBoth)
	defer stopClosingOnCancel()

	var activity *streamActivity
	var idled atomic.Bool
	if idleTimeout > 0 {
		activity = &streamActivity{}
		activity.markActive()

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
	countedCounters := withoutNil(counters)
	clientSource := observe(clientSide, activity, countedCounters, connectionBytes, uplink)
	remoteSource := observe(remoteSide, activity, countedCounters, connectionBytes, downlink)

	// Buffered for both, and the caller copies one direction itself, so the
	// errors still arrive in the order the directions ended.
	copyErrors := make(chan error, 2)
	go func() { copyErrors <- forwardStream(remoteSide, clientSource) }()
	copyErrors <- forwardStream(clientSide, remoteSource)

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

func withoutNil(counters []*stats.Counters) []*stats.Counters {
	withoutNil := make([]*stats.Counters, 0, len(counters))
	for _, counted := range counters {
		if counted != nil {
			withoutNil = append(withoutNil, counted)
		}
	}
	return withoutNil
}

type byteDirection bool

const (
	uplink   byteDirection = true
	downlink byteDirection = false
)

// The source itself when there is nothing to watch, so a connection with no
// idle timeout and no counters reads straight from the stream.
func observe(source io.Reader, activity *streamActivity, counters []*stats.Counters, connectionBytes *stats.ConversationBytes, direction byteDirection) io.Reader {
	if activity == nil && len(counters) == 0 && connectionBytes == nil {
		return source
	}
	return observedReader{source: source, activity: activity, counters: counters, connectionBytes: connectionBytes, direction: direction}
}

// Marks activity for the idle timeout and adds the bytes read to each
// counters entry as uplink or downlink. Both are optional.
type observedReader struct {
	source   io.Reader
	activity *streamActivity
	counters []*stats.Counters

	// Nil when the connection is not tracked.
	connectionBytes *stats.ConversationBytes
	direction       byteDirection
}

func (r observedReader) Read(buffer []byte) (int, error) {
	readSize, err := r.source.Read(buffer)
	if readSize > 0 {
		if r.activity != nil {
			r.activity.markActive()
		}
		if r.direction == uplink {
			for _, counted := range r.counters {
				counted.AddUplinkBytes(uint64(readSize))
			}
			r.connectionBytes.AddUplinkBytes(uint64(readSize))
		} else {
			for _, counted := range r.counters {
				counted.AddDownlinkBytes(uint64(readSize))
			}
			r.connectionBytes.AddDownlinkBytes(uint64(readSize))
		}
	}
	return readSize, err
}
