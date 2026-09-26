// Package udp holds what core's UDP packages share.
package udp

import "sync"

// Largest UDP payload over IPv4.
const MaxDatagramSize = 65507

// Every reply packet needs a full-size buffer only until the next layer has
// copied it on, so NAT and session take theirs from here instead of
// allocating one per packet.
var datagramBuffers = sync.Pool{New: func() any {
	buffer := make([]byte, MaxDatagramSize)
	return &buffer
}}

// AcquireDatagramBuffer returns a MaxDatagramSize buffer, so no read into it
// can be truncated. Hand it back with ReleaseDatagramBuffer, exactly once,
// after the last use of anything sliced from it.
func AcquireDatagramBuffer() *[]byte {
	return datagramBuffers.Get().(*[]byte)
}

func ReleaseDatagramBuffer(buffer *[]byte) {
	datagramBuffers.Put(buffer)
}
