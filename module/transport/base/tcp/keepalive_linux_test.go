//go:build linux

package tcp_test

import (
	"context"
	"net"
	"syscall"
	"testing"
	"time"

	"github.com/we-apranik/apranik/config/types"
	"github.com/we-apranik/apranik/module/transport/base/tcp"
	"github.com/we-apranik/apranik/spec"
)

func keepAliveOptions(t *testing.T, stream spec.Stream) (enabled, idle, interval, count int) {
	t.Helper()
	raw, err := stream.(*net.TCPConn).SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	raw.Control(func(fd uintptr) {
		enabled, _ = syscall.GetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_KEEPALIVE)
		idle, _ = syscall.GetsockoptInt(int(fd), syscall.IPPROTO_TCP, syscall.TCP_KEEPIDLE)
		interval, _ = syscall.GetsockoptInt(int(fd), syscall.IPPROTO_TCP, syscall.TCP_KEEPINTVL)
		count, _ = syscall.GetsockoptInt(int(fd), syscall.IPPROTO_TCP, syscall.TCP_KEEPCNT)
	})
	return enabled, idle, interval, count
}

// Both ends of a relay must carry all three keep-alive settings, not only the
// idle time.
func TestKeepAliveIntervalAndCountReachBothEnds(t *testing.T) {
	transport := &tcp.Transport{Settings: tcp.Settings{
		KeepAlive:         types.Pointer(types.Duration(20 * time.Second)),
		KeepAliveInterval: types.Pointer(types.Duration(7 * time.Second)),
		KeepAliveCount:    types.Pointer(4),
	}}
	listener, err := transport.NewStreamListener(endpoint("127.0.0.1", 0))
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	dialer, err := transport.NewStreamDialer(testResolver)
	if err != nil {
		t.Fatal(err)
	}

	accepted := make(chan spec.Stream, 1)
	go func() {
		stream, _ := listener.AcceptStream(context.Background())
		accepted <- stream
	}()
	client, err := dialer.DialStream(context.Background(), spec.NewEndpointFromNetAddr(listener.LocalAddr()).Destination())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	server := <-accepted
	defer server.Close()

	for name, stream := range map[string]spec.Stream{"dialed": client, "accepted": server} {
		enabled, idle, interval, count := keepAliveOptions(t, stream)
		if enabled != 1 || idle != 20 || interval != 7 || count != 4 {
			t.Errorf("%s: keep-alive on=%d idle=%ds interval=%ds count=%d, want on=1 idle=20s interval=7s count=4",
				name, enabled, idle, interval, count)
		}
	}
}

func TestNegativeKeepAliveDisablesProbesOnBothEnds(t *testing.T) {
	transport := &tcp.Transport{Settings: tcp.Settings{KeepAlive: types.Pointer(types.Duration(-1))}}
	listener, err := transport.NewStreamListener(endpoint("127.0.0.1", 0))
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	dialer, _ := transport.NewStreamDialer(testResolver)

	accepted := make(chan spec.Stream, 1)
	go func() {
		stream, _ := listener.AcceptStream(context.Background())
		accepted <- stream
	}()
	client, err := dialer.DialStream(context.Background(), spec.NewEndpointFromNetAddr(listener.LocalAddr()).Destination())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	server := <-accepted
	defer server.Close()

	for name, stream := range map[string]spec.Stream{"dialed": client, "accepted": server} {
		if enabled, _, _, _ := keepAliveOptions(t, stream); enabled != 0 {
			t.Errorf("%s: keep-alive is on, want off", name)
		}
	}
}
