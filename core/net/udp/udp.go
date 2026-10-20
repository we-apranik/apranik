// Package udp holds what core's UDP packages share.
package udp

import (
	"sync"
	"time"
)

// PROVISIONAL: 1 minute.
const DefaultRoutingCacheEntryIdleTimeout = time.Minute

// PROVISIONAL: 1024.
const DefaultRoutingCacheMaxEntriesPerSession = 1024

// PROVISIONAL: 1 minute.
const DefaultLiveTrackingUDPEntryIdleTimeout = time.Minute

// PROVISIONAL: 1024, about 0.5 MB a session in the worst case (an entry measured at about
const DefaultLiveTrackingUDPMaxEntriesPerSession = 1024

const LargestDatagramSize = 65507

// PROVISIONAL: 8192, above what any network carries without fragmenting (about 1472 bytes) and above
// DNS's largest (4096), and small enough that a session's few buffers stay a few tens of kilobytes.
const DefaultMaxDatagramSize = 8192

// BufferPool lends buffers of one size, so a reply is read into a buffer that is handed back once the
// next layer has copied it on, instead of one being allocated per datagram.
type BufferPool struct {
	size int
	pool sync.Pool
}

func NewBufferPool(size int) *BufferPool {
	if size < 1 || size > LargestDatagramSize {
		size = DefaultMaxDatagramSize
	}
	pool := &BufferPool{size: size}
	pool.pool.New = func() any {
		buffer := make([]byte, size)
		return &buffer
	}
	return pool
}

func (p *BufferPool) Size() int { return p.size }

func (p *BufferPool) Acquire() *[]byte { return p.pool.Get().(*[]byte) }

func (p *BufferPool) Release(buffer *[]byte) { p.pool.Put(buffer) }
