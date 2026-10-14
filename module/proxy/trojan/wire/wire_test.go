package wire_test

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/we-apranik/apranik/module/proxy/trojan/wire"
	"github.com/we-apranik/apranik/shared/socksaddr"
	"github.com/we-apranik/apranik/spec"
)

func domainDestination(t testing.TB, domain string, port uint16) spec.Destination {
	t.Helper()
	destination, err := spec.NewDestinationFromDomain(domain, port)
	if err != nil {
		t.Fatal(err)
	}
	return destination
}

func ipDestination(t testing.TB, ip string, port uint16) spec.Destination {
	t.Helper()
	destination, err := spec.NewDestinationFromIP(netip.MustParseAddr(ip), port)
	if err != nil {
		t.Fatal(err)
	}
	return destination
}

// The published SHA-224 test vector for the empty string, so the hash is
// checked against something this package did not compute.
func TestHashPasswordMatchesTheKnownVector(t *testing.T) {
	const sha224OfEmpty = "d14a028c2a3a2bc9476102bb288234c415a2b01f828ea62ac5b3e42f"
	hash := wire.HashPassword("")
	if string(hash[:]) != sha224OfEmpty {
		t.Errorf("HashPassword(\"\") = %s, want %s", hash[:], sha224OfEmpty)
	}
}

// A request written by hand from the Trojan description, byte by byte, so a
// mistake shared by encoder and decoder still shows up here.
func TestRequestMatchesHandWrittenBytes(t *testing.T) {
	hash := wire.HashPassword("")
	want := append([]byte{}, hash[:]...)
	want = append(want, '\r', '\n', 0x01, 0x03, 11)
	want = append(want, "example.com"...)
	want = append(want, 0x01, 0xBB, '\r', '\n')

	got, err := wire.AppendRequest(nil, wire.Request{
		PasswordHash: hash,
		Command:      wire.CommandConnect,
		Destination:  domainDestination(t, "example.com", 443),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("request =\n% X\nwant\n% X", got, want)
	}
}

func TestRequestRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		name        string
		command     byte
		destination spec.Destination
	}{
		{"CONNECT domain", wire.CommandConnect, domainDestination(t, "example.com", 443)},
		{"CONNECT IPv4", wire.CommandConnect, ipDestination(t, "93.184.216.34", 80)},
		{"UDP IPv6", wire.CommandUDPAssociate, ipDestination(t, "2001:db8::1", 53)},
		{"longest domain", wire.CommandConnect, domainDestination(t, strings.Repeat("a", 255), 1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sent := wire.Request{PasswordHash: wire.HashPassword("secret"), Command: tc.command, Destination: tc.destination}
			encoded, err := wire.AppendRequest(nil, sent)
			if err != nil {
				t.Fatal(err)
			}

			length, err := wire.RequestLength(encoded)
			if err != nil || length != len(encoded) {
				t.Errorf("RequestLength = %d, %v; want %d", length, err, len(encoded))
			}

			received, err := wire.ReadRequest(bytes.NewReader(encoded))
			if err != nil {
				t.Fatal(err)
			}
			if received != sent {
				t.Errorf("received %+v, sent %+v", received, sent)
			}
		})
	}
}

// One byte per Read: the parser must never assume a Read returns a whole
// field, and must leave the early payload that follows unread.
func TestRequestSplitAcrossReadsKeepsEarlyPayload(t *testing.T) {
	encoded, err := wire.AppendRequest(nil, wire.Request{
		PasswordHash: wire.HashPassword("secret"),
		Command:      wire.CommandConnect,
		Destination:  domainDestination(t, "example.com", 443),
	})
	if err != nil {
		t.Fatal(err)
	}
	stream := iotest.OneByteReader(bytes.NewReader(append(encoded, "early payload"...)))

	if _, err := wire.ReadRequest(stream); err != nil {
		t.Fatal(err)
	}
	rest, err := io.ReadAll(stream)
	if err != nil {
		t.Fatal(err)
	}
	if string(rest) != "early payload" {
		t.Errorf("after the header: %q, want %q", rest, "early payload")
	}
}

func TestRequestLengthNeedsMoreData(t *testing.T) {
	encoded, _ := wire.AppendRequest(nil, wire.Request{
		PasswordHash: wire.HashPassword("secret"),
		Command:      wire.CommandConnect,
		Destination:  domainDestination(t, "example.com", 443),
	})
	for size := range wire.PasswordHashLength + 5 {
		if _, err := wire.RequestLength(encoded[:size]); !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Errorf("RequestLength(%d bytes) = %v, want io.ErrUnexpectedEOF", size, err)
		}
	}
}

func validHeader() []byte {
	hash := wire.HashPassword("secret")
	header := append([]byte{}, hash[:]...)
	return append(header, '\r', '\n', 0x01, 0x01, 127, 0, 0, 1, 0x00, 0x50, '\r', '\n')
}

func TestMalformedRequestsAreRefused(t *testing.T) {
	hashEnd := wire.PasswordHashLength

	for _, tc := range []struct {
		name   string
		mutate func([]byte) []byte
		want   error
	}{
		{"hash one character short", func(h []byte) []byte { return append(h[:hashEnd-1:hashEnd-1], h[hashEnd:]...) }, wire.ErrMalformedPasswordHash},
		{"hash not hex", func(h []byte) []byte { h[0] = 'g'; return h }, wire.ErrMalformedPasswordHash},
		{"hash uppercase", func(h []byte) []byte { copy(h, "ABCDEF"); return h }, wire.ErrMalformedPasswordHash},
		{"first CRLF missing", func(h []byte) []byte { h[hashEnd] = ' '; return h }, wire.ErrMissingCRLF},
		{"last CRLF missing", func(h []byte) []byte { h[len(h)-1] = 'x'; return h }, wire.ErrMissingCRLF},
		{"unknown command", func(h []byte) []byte { h[hashEnd+2] = 0x02; return h }, wire.ErrUnknownCommand},
		{"unknown address type", func(h []byte) []byte { h[hashEnd+3] = 0x05; return h }, socksaddr.ErrUnsupportedAddressType},
		{"empty domain", func(h []byte) []byte {
			return append(h[:hashEnd+3:hashEnd+3], 0x03, 0, 0x00, 0x50, '\r', '\n')
		}, socksaddr.ErrEmptyDomain},
		{"truncated", func(h []byte) []byte { return h[:hashEnd+5] }, io.ErrUnexpectedEOF},
		{"empty", func(h []byte) []byte { return nil }, io.EOF},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := wire.ReadRequest(bytes.NewReader(tc.mutate(validHeader())))
			if !errors.Is(err, tc.want) {
				t.Errorf("ReadRequest = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestEncodingRefusesAnUnknownCommand(t *testing.T) {
	_, err := wire.AppendRequest(nil, wire.Request{Command: 0x02, Destination: domainDestination(t, "a", 1)})
	if !errors.Is(err, wire.ErrUnknownCommand) {
		t.Errorf("AppendRequest = %v, want ErrUnknownCommand", err)
	}
}

func TestPasswordHashNeverFormats(t *testing.T) {
	hash := wire.HashPassword("hunter2")
	request := wire.Request{PasswordHash: hash}
	for _, rendered := range []string{
		fmt.Sprintf("%v", hash), fmt.Sprintf("%s", hash), fmt.Sprintf("%#v", hash), fmt.Sprintf("%x", hash), fmt.Sprintf("%d", hash),
		fmt.Sprintf("%v", request), fmt.Sprintf("%+v", request), fmt.Sprintf("%#v", request),
	} {
		if strings.Contains(rendered, string(hash[:8])) {
			t.Errorf("formatting leaked the hash: %s", rendered)
		}
	}
}

func TestUDPFrameMatchesHandWrittenBytes(t *testing.T) {
	want := []byte{0x01, 8, 8, 8, 8, 0x00, 0x35, 0x00, 0x03, '\r', '\n', 'a', 'b', 'c'}
	got, err := wire.AppendUDPFrame(nil, ipDestination(t, "8.8.8.8", 53), []byte("abc"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("frame = % X, want % X", got, want)
	}
}

func TestUDPFrameRoundTrip(t *testing.T) {
	for _, size := range []int{0, 1, 1500, wire.MaxUDPPayload} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			payload := bytes.Repeat([]byte{0xA5}, size)
			destination := domainDestination(t, "dns.example", 53)

			frame, err := wire.AppendUDPFrame(nil, destination, payload)
			if err != nil {
				t.Fatal(err)
			}
			// Two frames back to back, one byte per Read: the second must
			// start exactly where the first ended.
			stream := bufio.NewReader(iotest.OneByteReader(bytes.NewReader(append(frame, frame...))))

			buffer := make([]byte, wire.MaxUDPPayload)
			for range 2 {
				n, from, err := wire.ReadUDPFrame(stream, buffer)
				if err != nil {
					t.Fatal(err)
				}
				if from != destination || !bytes.Equal(buffer[:n], payload) {
					t.Fatalf("read %d bytes from %s, want %d from %s", n, from, size, destination)
				}
			}
		})
	}
}

func TestUDPFrameTooLargeToEncode(t *testing.T) {
	_, err := wire.AppendUDPFrame(nil, ipDestination(t, "8.8.8.8", 53), make([]byte, wire.MaxUDPPayload+1))
	if !errors.Is(err, spec.ErrMessageTooLong) {
		t.Errorf("AppendUDPFrame = %v, want spec.ErrMessageTooLong", err)
	}
}

// A frame bigger than the caller's buffer is dropped whole, so the next
// frame still parses.
func TestUDPFrameTooLargeForTheBufferKeepsTheStreamAligned(t *testing.T) {
	destination := ipDestination(t, "8.8.8.8", 53)
	big, _ := wire.AppendUDPFrame(nil, destination, make([]byte, 100))
	small, _ := wire.AppendUDPFrame(nil, destination, []byte("next"))
	stream := bufio.NewReader(bytes.NewReader(append(big, small...)))

	buffer := make([]byte, 10)
	if _, _, err := wire.ReadUDPFrame(stream, buffer); !errors.Is(err, spec.ErrMessageTooLong) {
		t.Fatalf("first frame: %v, want spec.ErrMessageTooLong", err)
	}
	n, _, err := wire.ReadUDPFrame(stream, buffer)
	if err != nil || string(buffer[:n]) != "next" {
		t.Errorf("second frame = %q, %v; want \"next\"", buffer[:n], err)
	}
}

func TestMalformedUDPFramesAreRefused(t *testing.T) {
	for _, tc := range []struct {
		name  string
		frame []byte
		want  error
	}{
		{"unknown address type", []byte{0x09, 0, 0, '\r', '\n'}, socksaddr.ErrUnsupportedAddressType},
		{"empty domain", []byte{0x03, 0, 0, 53, 0, 0, '\r', '\n'}, socksaddr.ErrEmptyDomain},
		{"missing CRLF", []byte{0x01, 1, 1, 1, 1, 0, 53, 0, 0, 'x', 'y'}, wire.ErrMissingCRLF},
		{"payload shorter than its length", []byte{0x01, 1, 1, 1, 1, 0, 53, 0, 9, '\r', '\n', 'a'}, io.ErrUnexpectedEOF},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := wire.ReadUDPFrame(bufio.NewReader(bytes.NewReader(tc.frame)), make([]byte, 64))
			if !errors.Is(err, tc.want) {
				t.Errorf("ReadUDPFrame = %v, want %v", err, tc.want)
			}
		})
	}
}
