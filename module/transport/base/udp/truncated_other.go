//go:build !unix

package udp

// Without message flags, a datagram that fills the buffer may have been cut, so
// it is treated as cut: the one size lost is exactly the buffer's, and
// reporting a loss is better than passing on half a datagram.
func wasTruncated(_, size, bufferSize int) bool { return size >= bufferSize }
