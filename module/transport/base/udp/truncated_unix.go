//go:build unix

package udp

import "syscall"

// Whether the kernel cut a datagram to fit the buffer: it says so in the
// message flags, so a datagram of exactly the buffer's size is not mistaken
// for one that was cut.
func wasTruncated(flags, _, _ int) bool { return flags&syscall.MSG_TRUNC != 0 }
