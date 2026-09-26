// Package http is a sniffer: it recognises a plain HTTP/1 request in the
// first bytes of a stream and reports the host from its Host header, so
// routing can see a domain where the client only gave an IP.
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
	requestLineEnd      = []byte("\r\n")
	requestHeaderEnd    = []byte("\r\n\r\n")
	knownRequestMethods = []string{"GET", "POST", "PUT", "DELETE", "HEAD", "OPTIONS", "PATCH", "CONNECT", "TRACE"}
)

type Settings struct{}

func (*Settings) Validate() error    { return nil }
func (*Settings) Warnings() []string { return nil }

type Sniffer struct{ Settings }

func (*Sniffer) Info() spec.ModuleInfo {
	return spec.ModuleInfo{Name: "http", Description: "sniff host from an HTTP/1 request's Host header"}
}

func (*Sniffer) Sniff(payloadPrefix []byte) (spec.SniffResult, error) {
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

	lines := bytes.Split(payloadPrefix[:headerEnd], requestLineEnd)
	if !bytes.Contains(lines[0], []byte(" HTTP/1.")) {
		return spec.SniffResult{}, errNotHTTP
	}

	for _, line := range lines[1:] {
		name, value, found := bytes.Cut(line, []byte(":"))
		if found && strings.EqualFold(string(bytes.TrimSpace(name)), "host") {
			host, port := hostAndPort(string(bytes.TrimSpace(value)))
			return spec.SniffResult{Protocol: "http", Domain: host, Port: port}, nil
		}
	}
	return spec.SniffResult{Protocol: "http"}, nil
}

func NewSnifferModule() spec.Sniffer { return &Sniffer{} }

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
