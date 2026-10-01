package wire

import (
	"io"
	"net"
	"net/netip"
	"strings"
	"testing"

	"github.com/The-Apranik/Apranik/spec"
)

type pipeStream struct{ net.Conn }

func (pipeStream) CloseWrite() error { return spec.ErrNoHalfClose }

// streamSending returns a stream that reads sent, then EOF.
func streamSending(t *testing.T, sent string) spec.Stream {
	t.Helper()
	readEnd, writeEnd := net.Pipe()
	go func() {
		io.WriteString(writeEnd, sent)
		writeEnd.Close()
	}()
	t.Cleanup(func() { readEnd.Close() })
	return pipeStream{readEnd}
}

func TestReadHeadKeepsBytesSentAfterTheHead(t *testing.T) {
	stream := streamSending(t, "CONNECT example.com:443 HTTP/1.1\r\nHost: example.com:443\r\n\r\nTLS bytes")

	head, streamAfterHead, err := ReadHead(stream)
	if err != nil {
		t.Fatalf("ReadHead: %v", err)
	}
	if head.StartLine != "CONNECT example.com:443 HTTP/1.1" {
		t.Errorf("StartLine = %q", head.StartLine)
	}
	if got := head.HeaderValue("host"); got != "example.com:443" {
		t.Errorf(`HeaderValue("host") = %q`, got)
	}

	rest, err := io.ReadAll(streamAfterHead)
	if err != nil {
		t.Fatalf("reading the stream after the head: %v", err)
	}
	if string(rest) != "TLS bytes" {
		t.Errorf("stream after the head read %q, want %q", rest, "TLS bytes")
	}
}

func TestReadHeadRefusesOversizedHeads(t *testing.T) {
	for name, sent := range map[string]string{
		"too many header lines": "GET / HTTP/1.1\r\n" + strings.Repeat("X: y\r\n", maxHeaderLines+1) + "\r\n",
		"too many bytes":        "GET / HTTP/1.1\r\nX: " + strings.Repeat("y", maxHeadBytes) + "\r\n\r\n",
		"header without colon":  "GET / HTTP/1.1\r\nno colon here\r\n\r\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := ReadHead(streamSending(t, sent)); err == nil {
				t.Fatal("ReadHead accepted the head")
			}
		})
	}
}

func TestStreamAfterHeadReadsItsBytesFirst(t *testing.T) {
	stream := NewStreamAfterHead(streamSending(t, "world"), []byte("hello "))

	got, err := io.ReadAll(stream)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(got) != "hello world" {
		t.Errorf("read %q, want %q", got, "hello world")
	}
}

func TestNewStreamAfterHeadWithNothingToReadFirstReturnsTheStream(t *testing.T) {
	underlyingStream := streamSending(t, "")
	if NewStreamAfterHead(underlyingStream, nil) != underlyingStream {
		t.Error("NewStreamAfterHead wrapped a stream it had nothing to add to")
	}
}

func TestParseHostPort(t *testing.T) {
	for _, tc := range []struct {
		hostPort    string
		defaultPort uint16
		want        string
	}{
		{"example.com:443", 0, "example.com:443"},
		{"example.com", 80, "example.com:80"},
		{"192.0.2.1:8080", 0, "192.0.2.1:8080"},
		{"[2001:db8::1]:443", 0, "[2001:db8::1]:443"},
		{"[2001:db8::1]", 80, "[2001:db8::1]:80"},
	} {
		destination, err := ParseHostPort(tc.hostPort, tc.defaultPort)
		if err != nil {
			t.Errorf("ParseHostPort(%q): %v", tc.hostPort, err)
			continue
		}
		if destination.String() != tc.want {
			t.Errorf("ParseHostPort(%q) = %s, want %s", tc.hostPort, destination, tc.want)
		}
	}
}

func TestParseHostPortKeepsAnIPv6LiteralAnIP(t *testing.T) {
	destination, err := ParseHostPort("[2001:db8::1]", 80)
	if err != nil {
		t.Fatalf("ParseHostPort: %v", err)
	}
	ip, isIP := destination.IP()
	if !isIP || ip != netip.MustParseAddr("2001:db8::1") {
		t.Errorf("destination %s is not the IP 2001:db8::1", destination)
	}
}

func TestParseHostPortRefusesBadAddresses(t *testing.T) {
	for _, hostPort := range []string{"example.com", ":443", "example.com:0", "example.com:99999"} {
		if _, err := ParseHostPort(hostPort, 0); err == nil {
			t.Errorf("ParseHostPort(%q) accepted it", hostPort)
		}
	}
}

func TestBasicCredentialsMatch(t *testing.T) {
	value := BasicAuthorizationValue("alice", "secret")
	lowercaseScheme := "basic" + strings.TrimPrefix(value, "Basic")

	for _, tc := range []struct {
		name               string
		authorizationValue string
		want               bool
	}{
		{"exact", value, true},
		{"scheme in lower case", lowercaseScheme, true},
		{"wrong password", BasicAuthorizationValue("alice", "wrong"), false},
		{"other scheme", "Bearer token", false},
		{"empty", "", false},
	} {
		if got := BasicCredentialsMatch(tc.authorizationValue, "alice", "secret"); got != tc.want {
			t.Errorf("%s: BasicCredentialsMatch = %v, want %v", tc.name, got, tc.want)
		}
	}
}
