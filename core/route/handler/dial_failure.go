package handler

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"syscall"

	"github.com/The-Apranik/Apranik/core/dns"
	"github.com/The-Apranik/Apranik/core/stats"
)

// classifyDialError sorts a failed dial into the fixed list statistics count
// by. It reads only what the standard library and the resolver say, never an
// error's text. A proxy refusing a request in its own protocol says nothing
// a classifier can read, so that is DialOther, not DialHandshakeFailed, which
// covers a TLS handshake that failed: an alert from the peer or a certificate
// that did not verify.
func classifyDialError(err error) stats.DialFailureReason {
	var timeoutError interface{ Timeout() bool }
	switch {
	case errors.Is(err, context.Canceled):
		return stats.DialCanceled
	case errors.Is(err, dns.ErrLookupFailed):
		return stats.DialDNSFailed
	case errors.As(err, &timeoutError) && timeoutError.Timeout():
		return stats.DialTimeout
	case errors.Is(err, syscall.ECONNREFUSED):
		return stats.DialRefused
	case errors.Is(err, syscall.ECONNRESET):
		return stats.DialReset
	case errors.Is(err, syscall.ENETUNREACH), errors.Is(err, syscall.EHOSTUNREACH):
		return stats.DialUnreachable
	case isTLSHandshakeFailure(err):
		return stats.DialHandshakeFailed
	default:
		return stats.DialOther
	}
}

func isTLSHandshakeFailure(err error) bool {
	var verificationError *tls.CertificateVerificationError
	var unknownAuthority x509.UnknownAuthorityError
	var hostnameError x509.HostnameError
	var invalidCertificate x509.CertificateInvalidError
	var recordHeaderError tls.RecordHeaderError
	var operationError *net.OpError
	switch {
	case errors.As(err, &verificationError), errors.As(err, &unknownAuthority),
		errors.As(err, &hostnameError), errors.As(err, &invalidCertificate),
		errors.As(err, &recordHeaderError):
		return true
	case errors.As(err, &operationError):
		// The peer sent a TLS alert: crypto/tls and uTLS both report it as a "remote error".
		return operationError.Op == "remote error"
	default:
		return false
	}
}
