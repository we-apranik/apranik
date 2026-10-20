package doh

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/we-apranik/apranik/config/types"
	"github.com/we-apranik/apranik/spec"
)

const (
	DefaultPort = 443
	DefaultPath = "/dns-query"

	// PROVISIONAL.
	DefaultTimeout = 5 * time.Second

	dnsMessageMediaType = "application/dns-message"
	maxResponseLength   = 1<<16 - 1
	headerLength        = 12

	// A kept-alive connection unused for this long is closed. PROVISIONAL.
	idleConnectionTimeout = 30 * time.Second
	maxIdleConnections    = 4
)

type Settings struct {
	Server types.ServerAddress `json:"server"`

	Path string `json:"path"`

	Host string `json:"host"`

	Timeout *types.Duration `json:"timeout"`
}

func (s *Settings) Validate() error {
	if s.Server.IsZero() {
		return errors.New("doh: server is required")
	}
	if err := types.CheckPositive(s.Timeout); err != nil {
		return fmt.Errorf("doh: timeout %w", err)
	}
	if s.Path != "" {
		parsed, err := url.Parse(s.Path)
		if err != nil || !strings.HasPrefix(s.Path, "/") || parsed.Host != "" || parsed.Fragment != "" {
			return fmt.Errorf("doh: path %q must be an absolute path such as %q", s.Path, DefaultPath)
		}
	}
	if s.Host != "" {
		if _, err := types.ParseServerAddress(s.Host); err != nil {
			return fmt.Errorf("doh: host: %w", err)
		}
	}
	return nil
}

func (s *Settings) Warnings() []string { return nil }

type Server struct{ Settings }

func (*Server) Info() spec.ModuleInfo {
	return spec.ModuleInfo{Name: "doh", Description: "DNS over HTTPS (RFC 8484), HTTP/1.1"}
}

func (*Server) NeedsTLS() bool { return true }

// TransportInfo: HTTP/1.1 is all this module speaks, so the TLS layer under it
// must not offer h2, or a server choosing it fails every query.
func (*Server) TransportInfo() spec.TransportInfo {
	return spec.TransportInfo{ApplicationProtocols: []string{"http/1.1"}}
}

func (s *Server) NewStreamDNSServer(dialer spec.StreamDialer) (spec.DNSServer, error) {
	if dialer == nil {
		return nil, errors.New("doh: no dialer")
	}
	destination, err := s.Server.Destination(DefaultPort)
	if err != nil {
		return nil, fmt.Errorf("doh: server: %w", err)
	}
	timeout := time.Duration(types.ValueOrDefault(s.Timeout, types.Duration(DefaultTimeout)))
	path := s.Path
	if path == "" {
		path = DefaultPath
	}
	host := s.Host
	if host == "" {
		host = destination.String()
		if destination.Port() == DefaultPort {
			host = destination.Host()
			if ip, isIP := destination.IP(); isIP && ip.Is6() {
				host = "[" + host + "]"
			}
		}
	}

	// The scheme is http: the TLS is already under the stream the dialer
	// returns. No proxy from the environment, no compression: nothing but
	// the dialer decides where this goes.
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return dialer.DialStream(ctx, destination)
		},
		Proxy:                  nil,
		DisableCompression:     true,
		ForceAttemptHTTP2:      false,
		MaxIdleConns:           maxIdleConnections,
		MaxIdleConnsPerHost:    maxIdleConnections,
		IdleConnTimeout:        idleConnectionTimeout,
		ResponseHeaderTimeout:  timeout,
		MaxResponseHeaderBytes: 16 << 10,
	}
	return &client{
		httpClient: &http.Client{Transport: transport, CheckRedirect: refuseRedirects},
		endpoint:   (&url.URL{Scheme: "http", Host: host, Path: path}).String(),
		timeout:    timeout,
	}, nil
}

func refuseRedirects(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

func NewStreamDNSServerModule() spec.StreamDNSServerModule { return &Server{} }

type client struct {
	httpClient *http.Client
	endpoint   string
	timeout    time.Duration
}

func (c *client) Query(ctx context.Context, queryMessage []byte) ([]byte, error) {
	if len(queryMessage) < headerLength || len(queryMessage) > maxResponseLength {
		return nil, errors.New("doh: a DNS message is 12 to 65535 bytes")
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(queryMessage))
	if err != nil {
		return nil, fmt.Errorf("doh: %w", err)
	}
	request.Header.Set("Content-Type", dnsMessageMediaType)
	request.Header.Set("Accept", dnsMessageMediaType)
	request.Header.Set("User-Agent", "")

	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("doh: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		io.Copy(io.Discard, io.LimitReader(response.Body, maxResponseLength))
		return nil, fmt.Errorf("doh: server answered %s", response.Status)
	}
	if mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type")); err != nil || mediaType != dnsMessageMediaType {
		return nil, fmt.Errorf("doh: server answered %q, not %s", response.Header.Get("Content-Type"), dnsMessageMediaType)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseLength+1))
	if err != nil {
		return nil, fmt.Errorf("doh: reading the answer: %w", err)
	}
	if len(body) > maxResponseLength {
		return nil, errors.New("doh: answer longer than a DNS message can be")
	}
	return body, nil
}

func (c *client) Close() error {
	c.httpClient.CloseIdleConnections()
	return nil
}
