package outbound

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"time"

	"github.com/The-Apranik/Apranik/core/component"
	"github.com/The-Apranik/Apranik/shared/httpstatusranges"
	"github.com/The-Apranik/Apranik/spec"
)

// DelayTester sends one HTTP request through an outbound and times it: the one
// place a delay is measured. A group's prober calls it on a timer, an API calls
// it once on request. It has no configuration and no defaults: whoever uses it
// says what to request, which statuses pass and how long to wait.
type DelayTester struct {
	destination      spec.Destination
	hostHeader       string
	serverName       string
	requestURL       *url.URL
	useTLS           bool
	expectedStatuses []httpstatusranges.Range

	// Nil means the system's roots; only a test sets it.
	rootCAs *x509.CertPool
}

// NewDelayTester takes the URL apart once. The URL must be http or https and
// name a host; its scheme decides whether the request is encrypted toward the
// target. expectedStatuses must not be empty: 100-599 passes any response.
func NewDelayTester(rawURL string, expectedStatuses []httpstatusranges.Range) (DelayTester, error) {
	if rawURL == "" {
		return DelayTester{}, errors.New("a delay test needs a url")
	}
	requestURL, err := url.Parse(rawURL)
	if err != nil {
		return DelayTester{}, fmt.Errorf("delay test url %q: %w", rawURL, err)
	}
	var useTLS bool
	var defaultPort uint16
	switch requestURL.Scheme {
	case "http":
		defaultPort = 80
	case "https":
		useTLS, defaultPort = true, 443
	default:
		return DelayTester{}, fmt.Errorf("delay test url %q must start with http:// or https://", rawURL)
	}
	host := requestURL.Hostname()
	if host == "" {
		return DelayTester{}, fmt.Errorf("delay test url %q names no host", rawURL)
	}
	if len(expectedStatuses) == 0 {
		return DelayTester{}, errors.New("a delay test needs the statuses that pass; 100-599 passes any response")
	}

	port := defaultPort
	if text := requestURL.Port(); text != "" {
		parsed, err := strconv.ParseUint(text, 10, 16)
		if err != nil || parsed == 0 {
			return DelayTester{}, fmt.Errorf("delay test url %q has an invalid port %q", rawURL, text)
		}
		port = uint16(parsed)
	}

	var destination spec.Destination
	if address, parseErr := netip.ParseAddr(host); parseErr == nil {
		destination, err = spec.NewDestinationFromIP(address, port)
	} else {
		destination, err = spec.NewDestinationFromDomain(host, port)
	}
	if err != nil {
		return DelayTester{}, fmt.Errorf("delay test url %q: %w", rawURL, err)
	}

	return DelayTester{
		destination:      destination,
		hostHeader:       requestURL.Host,
		serverName:       host,
		requestURL:       requestURL,
		useTLS:           useTLS,
		expectedStatuses: expectedStatuses,
	}, nil
}

var errDelayTestStatus = errors.New("the delay test target did not answer with an expected status")

// MeasureDelay is the time from asking through for a connection to reading the
// target's status line and headers, so it includes every handshake a real
// connection pays. Counting only the connection would miss the proxy
// handshake, which a proxy client may send with the first write. A timeout of
// any kind comes back as an error wrapping context.DeadlineExceeded.
func (t DelayTester) MeasureDelay(ctx context.Context, through component.Outbound, timeout time.Duration) (time.Duration, error) {
	if timeout <= 0 {
		return 0, errors.New("a delay test needs a timeout")
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	elapsed, err := t.measure(ctx, through)
	if err != nil {
		return 0, reportTimeout(ctx, err)
	}
	return elapsed, nil
}

func (t DelayTester) measure(ctx context.Context, through component.Outbound) (time.Duration, error) {
	start := time.Now()
	stream, err := through.DialStream(ctx, spec.Metadata{Network: spec.NetworkTCP, Destination: t.destination})
	if err != nil {
		return 0, err
	}
	defer stream.Close()
	// Ends a blocked read at once when the caller gives up, not at the timeout.
	defer context.AfterFunc(ctx, func() { stream.Close() })()
	deadline, _ := ctx.Deadline()
	if err := stream.SetDeadline(deadline); err != nil {
		return 0, err
	}

	var connection net.Conn = stream
	if t.useTLS {
		secured := tls.Client(stream, &tls.Config{ServerName: t.serverName, NextProtos: []string{"http/1.1"}, MinVersion: tls.VersionTLS12, RootCAs: t.rootCAs})
		if err := secured.HandshakeContext(ctx); err != nil {
			return 0, fmt.Errorf("tls: %w", err)
		}
		connection = secured
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, t.requestURL.String(), nil)
	if err != nil {
		return 0, err
	}
	request.Host = t.hostHeader
	// An empty value stops net/http from naming itself in the request.
	request.Header["User-Agent"] = []string{""}
	request.Close = true
	if err := request.Write(connection); err != nil {
		return 0, err
	}

	response, err := http.ReadResponse(bufio.NewReader(connection), request)
	if err != nil {
		return 0, err
	}
	response.Body.Close()
	elapsed := time.Since(start)
	if !httpstatusranges.Contains(t.expectedStatuses, response.StatusCode) {
		return 0, fmt.Errorf("%w: got %d, expected %s", errDelayTestStatus, response.StatusCode, httpstatusranges.String(t.expectedStatuses))
	}
	return elapsed, nil
}

// A deadline can end a test as the context's own error or as a network error
// from a read, a write or a dial; callers get one answer for both.
func reportTimeout(ctx context.Context, err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("the delay test timed out: %w", err)
	}
	var networkError net.Error
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || (errors.As(err, &networkError) && networkError.Timeout()) {
		return fmt.Errorf("the delay test timed out (%v): %w", err, context.DeadlineExceeded)
	}
	return err
}
