//go:build unix

package udp

import "syscall"

func wasTruncated(flags, _, _ int) bool { return flags&syscall.MSG_TRUNC != 0 }
