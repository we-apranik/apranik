package handler

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"os"
	"syscall"
	"testing"

	"github.com/we-apranik/apranik/core/dns"
	"github.com/we-apranik/apranik/core/stats"
)

func TestADialErrorIsSortedByWhatTheStandardLibraryAndTheResolverSay(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want stats.DialFailureReason
	}{
		{"deadline", fmt.Errorf("tcp: dial: %w", context.DeadlineExceeded), stats.DialTimeout},
		{"read deadline", &net.OpError{Op: "read", Err: os.ErrDeadlineExceeded}, stats.DialTimeout},
		{"refused", &net.OpError{Op: "dial", Err: os.NewSyscallError("connect", syscall.ECONNREFUSED)}, stats.DialRefused},
		{"reset", &net.OpError{Op: "read", Err: os.NewSyscallError("read", syscall.ECONNRESET)}, stats.DialReset},
		{"network unreachable", &net.OpError{Op: "dial", Err: os.NewSyscallError("connect", syscall.ENETUNREACH)}, stats.DialUnreachable},
		{"host unreachable", &net.OpError{Op: "dial", Err: os.NewSyscallError("connect", syscall.EHOSTUNREACH)}, stats.DialUnreachable},
		{"canceled", fmt.Errorf("outbound: %w", context.Canceled), stats.DialCanceled},
		{"name did not resolve", fmt.Errorf("tcp: resolve example.com: %w", fmt.Errorf("%w: no answer", dns.ErrLookupFailed)), stats.DialDNSFailed},
		{"certificate not trusted", fmt.Errorf("tls: %w", x509.UnknownAuthorityError{}), stats.DialHandshakeFailed},
		{"certificate verification", &tls.CertificateVerificationError{Err: errors.New("expired")}, stats.DialHandshakeFailed},
		{"alert from the server", &net.OpError{Op: "remote error", Err: errors.New("tls: handshake failure")}, stats.DialHandshakeFailed},
		{"every member of a group failed", errors.Join(errors.New("first"), &net.OpError{Op: "dial", Err: os.NewSyscallError("connect", syscall.ECONNREFUSED)}), stats.DialRefused},
		{"something else", errors.New("proxy said no"), stats.DialOther},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyDialError(tc.err); got != tc.want {
				t.Errorf("classifyDialError(%v) = %s, want %s", tc.err, got, tc.want)
			}
		})
	}
}
