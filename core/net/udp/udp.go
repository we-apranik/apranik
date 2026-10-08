// Package udp holds what core's UDP packages share.
package udp

import (
	"sync"
	"time"
)

// How long a UDP session keeps one destination's routing decision after the
// last packet to it. PROVISIONAL: 1 minute. The inbound's
// udp_routing_cache_entry_idle_timeout sets it.
const DefaultRoutingCacheEntryIdleTimeout = time.Minute

// How many destinations a UDP session remembers a routing decision for: when
// it is full, the destination used longest ago is forgotten. PROVISIONAL: 1024.
// The inbound's udp_routing_cache_max_entries_per_session sets it.
const DefaultRoutingCacheMaxEntriesPerSession = 1024

// How long a UDP destination's live entry survives silence in either
// direction. PROVISIONAL: 1 minute. stats.tracking.udp_entry_idle_timeout sets it.
const DefaultLiveTrackingUDPEntryIdleTimeout = time.Minute

// How many destinations of one UDP session are listed as live at once: when it
// is full, the entry used longest ago, in either direction, ends. PROVISIONAL:
// 1024, about 0.5 MB a session in the worst case (an entry measured at about
// 490 bytes). stats.tracking.udp_max_entries_per_session sets it.
const DefaultLiveTrackingUDPMaxEntriesPerSession = 1024

// Largest UDP payload over IPv4: no setting may ask for more.
const LargestDatagramSize = 65507

// How big a UDP datagram core's buffers hold: a datagram over it is dropped and
// counted, not cut short. PROVISIONAL: 8192, above what any network carries
// without fragmenting (about 1472 bytes) and above DNS's largest (4096), and
// small enough that a session's few buffers stay a few tens of kilobytes. The
// inbound's udp_max_datagram_size, freedom's nat.max_datagram_size and socks5's
// udp_max_datagram_size set it.
const DefaultMaxDatagramSize = 8192

// BufferPool lends buffers of one size, so a reply is read into a buffer that
// is handed back once the next layer has copied it on, instead of one being
// allocated per datagram. Safe for concurrent use.
type BufferPool struct {
	size int
	pool sync.Pool
}

// NewBufferPool makes a pool of size-byte buffers. A size below one, or above
// the largest datagram, means DefaultMaxDatagramSize.
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

// Acquire returns a Size() buffer. Hand it back with Release, exactly once,
// after the last use of anything sliced from it.
func (p *BufferPool) Acquire() *[]byte { return p.pool.Get().(*[]byte) }

func (p *BufferPool) Release(buffer *[]byte) { p.pool.Put(buffer) }
