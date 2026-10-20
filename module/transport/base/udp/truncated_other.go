//go:build !unix

package udp

func wasTruncated(_, size, bufferSize int) bool { return size >= bufferSize }
