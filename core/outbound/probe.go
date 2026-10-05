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
	"github.com/The-Apranik/Apranik/spec"
)

const (
	DefaultHTTPProbeURL  = "http://www.gstatic.com/generate_204"
	DefaultHTTPSProbeURL = "https://www.gstatic.com/generate_204"

	// PROVISIONAL.
	DefaultProbeInterval = 5 * time.Minute
	DefaultProbeTimeout  = 10 * time.Second
)

// ProbeConfig is the request a group sends through each member. Zero fields
// mean the defaults above.
type ProbeConfig struct {
	// Whether the probe speaks TLS to its target, so URL must be https.
	UseTLS bool

	URL string

	Interval time.Duration
	Timeout  time.Duration
}

// probeTarget is a probe's URL taken apart once, when the group is built.
type probeTarget struct {
	destination spec.Destination
	hostHeader  string
	serverName  string
	requestURL  *url.URL
	useTLS      bool

	// Nil means the system's roots; only a test sets it.
	rootCAs *x509.CertPool
}

func newProbeTarget(config ProbeConfig) (probeTarget, error) {
	rawURL := config.URL
	if rawURL == "" {
		rawURL = DefaultHTTPProbeURL
		if config.UseTLS {
			rawURL = DefaultHTTPSProbeURL
		}
	}

	requestURL, err := url.Parse(rawURL)
	if err != nil {
		return probeTarget{}, fmt.Errorf("probe url %q: %w", rawURL, err)
	}
	wantScheme, defaultPort := "http", uint16(80)
	if config.UseTLS {
		wantScheme, defaultPort = "https", 443
	}
	if requestURL.Scheme != wantScheme {
		return probeTarget{}, fmt.Errorf("probe url %q must start with %s://", rawURL, wantScheme)
	}
	host := requestURL.Hostname()
	if host == "" {
		return probeTarget{}, fmt.Errorf("probe url %q names no host", rawURL)
	}

	port := defaultPort
	if text := requestURL.Port(); text != "" {
		parsed, err := strconv.ParseUint(text, 10, 16)
		if err != nil || parsed == 0 {
			return probeTarget{}, fmt.Errorf("probe url %q has an invalid port %q", rawURL, text)
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
		return probeTarget{}, fmt.Errorf("probe url %q: %w", rawURL, err)
	}

	return probeTarget{
		destination: destination,
		hostHeader:  requestURL.Host,
		serverName:  host,
		requestURL:  requestURL,
		useTLS:      config.UseTLS,
	}, nil
}

var errProbeStatus = errors.New("the probe target did not answer with a 2xx status")

// measure is the time from asking the member for a connection to reading the
// target's status line and headers, so it includes every handshake a real
// connection pays. Counting only the connection would miss the proxy
// handshake, which a proxy client may send with the first write.
func (t probeTarget) measure(ctx context.Context, member component.Outbound, timeout time.Duration) (time.Duration, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()
	stream, err := member.DialStream(ctx, spec.Metadata{Network: spec.NetworkTCP, Destination: t.destination})
	if err != nil {
		return 0, err
	}
	defer stream.Close()
	// Ends a blocked read at once when the group is closed, not at the timeout.
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
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return 0, fmt.Errorf("%w: got %d", errProbeStatus, response.StatusCode)
	}
	return elapsed, nil
}
