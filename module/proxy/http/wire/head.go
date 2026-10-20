// Package wire is the HTTP proxy byte format: reading a request or response
// head, Basic credentials, and host:port addresses. It is kept apart from the
// protocol so it can be tested byte for byte.
package wire

import (
	"bufio"
	"bytes"
	"fmt"
	"strings"
	"sync"

	"github.com/we-apranik/apranik/spec"
)

const (
	headReaderBufferSize = 8 << 10
	maxHeadBytes         = 64 << 10
	maxHeaderLines       = 128
)

var headReaderPool = sync.Pool{New: func() any { return bufio.NewReaderSize(nil, headReaderBufferSize) }}

type Head struct {
	StartLine   string
	HeaderLines []string
}

func (h Head) HeaderValue(headerName string) string {
	for _, headerLine := range h.HeaderLines {
		if strings.EqualFold(HeaderLineName(headerLine), headerName) {
			_, value, _ := strings.Cut(headerLine, ":")
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func HeaderLineName(headerLine string) string {
	name, _, _ := strings.Cut(headerLine, ":")
	return strings.TrimSpace(name)
}

func ReadHead(underlyingStream spec.Stream) (Head, spec.Stream, error) {
	headReader := headReaderPool.Get().(*bufio.Reader)
	headReader.Reset(underlyingStream)
	defer func() {
		headReader.Reset(nil)
		headReaderPool.Put(headReader)
	}()

	head, err := readHead(headReader)
	if err != nil {
		return Head{}, nil, err
	}

	payloadAlreadyRead, _ := headReader.Peek(headReader.Buffered())
	return head, NewStreamAfterHead(underlyingStream, bytes.Clone(payloadAlreadyRead)), nil
}

func readHead(headReader *bufio.Reader) (Head, error) {
	var head Head
	headBytesRead := 0

	startLine, err := readHeadLine(headReader, &headBytesRead)
	if err != nil {
		return Head{}, err
	}
	head.StartLine = startLine

	for {
		headerLine, err := readHeadLine(headReader, &headBytesRead)
		switch {
		case err != nil:
			return Head{}, err
		case headerLine == "":
			return head, nil
		case !strings.Contains(headerLine, ":"):
			return Head{}, fmt.Errorf("http: malformed header line %q", headerLine)
		case len(head.HeaderLines) == maxHeaderLines:
			return Head{}, fmt.Errorf("http: more than %d header lines", maxHeaderLines)
		}
		head.HeaderLines = append(head.HeaderLines, headerLine)
	}
}

func readHeadLine(headReader *bufio.Reader, headBytesRead *int) (string, error) {
	line, err := headReader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("http: reading head: %w", err)
	}
	if *headBytesRead += len(line); *headBytesRead > maxHeadBytes {
		return "", fmt.Errorf("http: head longer than %d bytes", maxHeadBytes)
	}
	return strings.TrimRight(line, "\r\n"), nil
}
