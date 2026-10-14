package http

import (
	"bytes"
	"fmt"
	"net/url"
	"strings"

	"github.com/we-apranik/apranik/module/proxy/http/wire"
	"github.com/we-apranik/apranik/spec"
)

const defaultHTTPPort = 80

// Reads a plain `GET http://host/path` request: the destination comes from the
// URL, and the request is rewritten for the origin and placed ahead of the
// rest of the stream. Nothing is answered here; the origin's response is what
// the client waits for.
func acceptAbsoluteFormRequest(streamAfterHead spec.Stream, metadata spec.Metadata, httpVersion, requestMethod, requestTarget string, headerLines []string) (spec.Stream, spec.Metadata, error) {
	absoluteURL, err := url.Parse(requestTarget)
	if err != nil || absoluteURL.Scheme == "" || absoluteURL.Host == "" {
		writeStatusResponse(streamAfterHead, httpVersion, statusBadRequest)
		return nil, spec.Metadata{}, fmt.Errorf("http: %s %q is neither CONNECT nor an absolute-form request", requestMethod, requestTarget)
	}
	if !strings.EqualFold(absoluteURL.Scheme, "http") {
		writeStatusResponse(streamAfterHead, httpVersion, statusBadRequest)
		return nil, spec.Metadata{}, fmt.Errorf("http: cannot forward scheme %q, use CONNECT", absoluteURL.Scheme)
	}

	destination, err := wire.ParseHostPort(absoluteURL.Host, defaultHTTPPort)
	if err != nil {
		writeStatusResponse(streamAfterHead, httpVersion, statusBadRequest)
		return nil, spec.Metadata{}, err
	}
	metadata.Destination = destination

	originRequestHead := rewriteHeadForOrigin(requestMethod, absoluteURL, httpVersion, headerLines)
	return wire.NewStreamAfterHead(streamAfterHead, originRequestHead), metadata, nil
}

// Rewrites the request line for the origin and drops the hop-by-hop headers a
// proxy must not forward. Connection: close is forced because the underlying
// stream becomes a tunnel to one origin, and a keep-alive request naming a
// different host would be delivered to the wrong one.
func rewriteHeadForOrigin(requestMethod string, absoluteURL *url.URL, httpVersion string, headerLines []string) []byte {
	var originRequestHead bytes.Buffer
	fmt.Fprintf(&originRequestHead, "%s %s %s\r\n", requestMethod, absoluteURL.RequestURI(), httpVersion)

	for _, headerLine := range headerLines {
		switch strings.ToLower(wire.HeaderLineName(headerLine)) {
		case "proxy-connection", "proxy-authorization", "connection", "keep-alive":
			continue
		}
		originRequestHead.WriteString(headerLine)
		originRequestHead.WriteString("\r\n")
	}

	originRequestHead.WriteString("Connection: close\r\n\r\n")
	return originRequestHead.Bytes()
}
