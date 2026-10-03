// Package http is a sniffer: it recognises a plain HTTP/1 request in the
// first bytes of a stream and reports the host from its Host header, so
// routing can see a domain where the client only gave an IP, and the
// request's headers with ":method" and ":path".
package http

import (
	"bytes"
	"errors"
	"net"
	"strconv"
	"strings"

	"github.com/The-Apranik/Apranik/spec"
)

const maxRequestHeaderSize = 16 << 10

var (
	errNotHTTP          = errors.New("http: not an HTTP/1 request")
	errHeaderTooLarge   = errors.New("http: request header larger than the sniffer reads")
	requestHeaderEnd    = []byte("\r\n\r\n")
	knownRequestMethods = []string{"GET", "POST", "PUT", "DELETE", "HEAD", "OPTIONS", "PATCH", "CONNECT", "TRACE"}
)

type Settings struct{}

func (*Settings) Validate() error    { return nil }
func (*Settings) Warnings() []string { return nil }

type Sniffer struct{ Settings }

func (*Sniffer) Info() spec.ModuleInfo {
	return spec.ModuleInfo{Name: "http", Description: "sniff host and request headers from an HTTP/1 request"}
}

// Nothing to prepare: the sniffer reads only the bytes it is given.
func (*Sniffer) NewSniffer() (spec.Sniffer, error) { return hostHeaderSniffer{}, nil }

func NewSnifferModule() spec.SnifferModule { return &Sniffer{} }

type hostHeaderSniffer struct{}

func (hostHeaderSniffer) Sniff(payloadPrefix []byte) (spec.SniffResult, error) {
	if err := startsWithKnownMethod(payloadPrefix); err != nil {
		return spec.SniffResult{}, err
	}

	headerEnd := bytes.Index(payloadPrefix, requestHeaderEnd)
	if headerEnd < 0 {
		if len(payloadPrefix) >= maxRequestHeaderSize {
			return spec.SniffResult{}, errHeaderTooLarge
		}
		return spec.SniffResult{}, spec.ErrSniffNeedMoreData
	}

	// One conversion; every name and value below is a slice of it.
	header := string(payloadPrefix[:headerEnd])
	requestLine, headerLines, _ := strings.Cut(header, "\r\n")
	method, rest, _ := strings.Cut(requestLine, " ")
	path, version, _ := strings.Cut(rest, " ")
	if path == "" || !strings.HasPrefix(version, "HTTP/1.") {
		return spec.SniffResult{}, errNotHTTP
	}

	result := spec.SniffResult{
		Protocol:       "http",
		RequestHeaders: make(map[string]string, strings.Count(headerLines, "\r\n")+3),
	}
	result.RequestHeaders[":method"] = method
	result.RequestHeaders[":path"] = path
	hostSeen := false
	for line := range strings.SplitSeq(headerLines, "\r\n") {
		name, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		headerName := strings.ToLower(strings.TrimSpace(name))
		headerValue := strings.TrimSpace(value)
		if previousValue, repeated := result.RequestHeaders[headerName]; repeated {
			result.RequestHeaders[headerName] = previousValue + ", " + headerValue
		} else {
			result.RequestHeaders[headerName] = headerValue
		}
		if headerName == "host" && !hostSeen {
			hostSeen = true
			result.Domain, result.Port = hostAndPort(headerValue)
		}
	}
	return result, nil
}

// Decides early, from the method alone, so a stream that is plainly not HTTP
// is not held waiting for a header end that will never come.
func startsWithKnownMethod(payloadPrefix []byte) error {
	for _, method := range knownRequestMethods {
		methodAndSpace := method + " "
		if len(payloadPrefix) >= len(methodAndSpace) {
			if string(payloadPrefix[:len(methodAndSpace)]) == methodAndSpace {
				return nil
			}
			continue
		}
		if strings.HasPrefix(methodAndSpace, string(payloadPrefix)) {
			return spec.ErrSniffNeedMoreData
		}
	}
	return errNotHTTP
}

// A Host without a port, or with one that does not parse, reports port zero.
func hostAndPort(hostHeader string) (string, uint16) {
	host, portText, err := net.SplitHostPort(hostHeader)
	if err != nil {
		return strings.TrimSuffix(strings.TrimPrefix(hostHeader, "["), "]"), 0
	}
	port, err := strconv.ParseUint(portText, 10, 16)
	if err != nil {
		return host, 0
	}
	return host, uint16(port)
}
