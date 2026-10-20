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

	"github.com/we-apranik/apranik/module/proxy/vless/wire"
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

var sequentialUUID = wire.UUID{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}

func TestRequestMatchesHandWrittenBytes(t *testing.T) {
	want := []byte{0x00}
	want = append(want, sequentialUUID[:]...)
	want = append(want, 0x00, 0x01, 0x01, 0xBB, 0x02, 11)
	want = append(want, "example.com"...)

	got, err := wire.AppendRequestHeader(nil, wire.Request{
		UUID:        sequentialUUID,
		Command:     wire.CommandTCP,
		Destination: domainDestination(t, "example.com", 443),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("request = % X, want % X", got, want)
	}
}

func TestMuxRequestMatchesHandWrittenBytes(t *testing.T) {
	want := []byte{0x00}
	want = append(want, sequentialUUID[:]...)
	want = append(want, 0x00, 0x03)

	got, err := wire.AppendRequestHeader(nil, wire.Request{UUID: sequentialUUID, Command: wire.CommandMux})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("request = % X, want % X", got, want)
	}
}

func TestMuxRequestRoundTripsAndLeavesWhatFollowsUnread(t *testing.T) {
	encoded, err := wire.AppendRequestHeader(nil, wire.Request{UUID: sequentialUUID, Command: wire.CommandMux})
	if err != nil {
		t.Fatal(err)
	}
	reader := bytes.NewReader(append(encoded, "frames"...))
	request, err := wire.ReadRequestHeader(reader)
	if err != nil {
		t.Fatal(err)
	}
	if request.Command != wire.CommandMux || request.UUID != sequentialUUID || request.Destination.IsValid() {
		t.Errorf("request = %+v, want the Mux command with the UUID and no destination", request)
	}
	if rest, _ := io.ReadAll(reader); string(rest) != "frames" {
		t.Errorf("left %q, want the bytes after the command", rest)
	}
}

func TestRequestRoundTripsEveryAddressTypeAndBothCommands(t *testing.T) {
	destinations := map[string]spec.Destination{
		"ipv4":   ipDestination(t, "192.0.2.1", 53),
		"ipv6":   ipDestination(t, "2001:db8::1", 443),
		"domain": domainDestination(t, "example.com", 8443),
	}
	for name, destination := range destinations {
		for _, command := range []byte{wire.CommandTCP, wire.CommandUDP} {
			t.Run(fmt.Sprintf("%s/command %d", name, command), func(t *testing.T) {
				request := wire.Request{UUID: sequentialUUID, Command: command, Destination: destination}
				encoded, err := wire.AppendRequestHeader(nil, request)
				if err != nil {
					t.Fatal(err)
				}
				decoded, err := wire.ReadRequestHeader(bytes.NewReader(encoded))
				if err != nil {
					t.Fatal(err)
				}
				if decoded != request {
					t.Errorf("decoded %+v, want %+v", decoded, request)
				}
			})
		}
	}
}

func TestReadRequestHeaderStopsAtTheEndOfTheHeader(t *testing.T) {
	encoded, err := wire.AppendRequestHeader(nil, wire.Request{UUID: sequentialUUID, Command: wire.CommandTCP, Destination: ipDestination(t, "192.0.2.1", 80)})
	if err != nil {
		t.Fatal(err)
	}
	reader := bytes.NewReader(append(encoded, "payload"...))
	if _, err := wire.ReadRequestHeader(reader); err != nil {
		t.Fatal(err)
	}
	rest, _ := io.ReadAll(reader)
	if string(rest) != "payload" {
		t.Errorf("left %q after the header, want the payload", rest)
	}
}

func TestMuxAndReverseCarryNoDestination(t *testing.T) {
	for _, command := range []byte{wire.CommandMux, wire.CommandReverse} {
		input := []byte{0x00}
		input = append(input, sequentialUUID[:]...)
		input = append(input, 0x00, command, 'X')
		reader := bytes.NewReader(input)

		request, err := wire.ReadRequestHeader(reader)
		if err != nil {
			t.Fatalf("command %d: %v", command, err)
		}
		if request.Command != command || request.Destination != (spec.Destination{}) {
			t.Errorf("command %d: got %+v, want the command and no destination", command, request)
		}
		if rest, _ := io.ReadAll(reader); string(rest) != "X" {
			t.Errorf("command %d: left %q, want the first payload byte", command, rest)
		}
	}
}

func TestFlowRoundTripsThroughTheAddons(t *testing.T) {
	request := wire.Request{
		UUID: sequentialUUID, Flow: "xtls-rprx-vision", Command: wire.CommandTCP,
		Destination: ipDestination(t, "192.0.2.1", 80),
	}
	encoded, err := wire.AppendRequestHeader(nil, request)
	if err != nil {
		t.Fatal(err)
	}
	if want := append([]byte{18, 0x0A, 16}, "xtls-rprx-vision"...); !bytes.Contains(encoded, want) {
		t.Errorf("request = % X, want it to hold the addons % X", encoded, want)
	}
	decoded, err := wire.ReadRequestHeader(bytes.NewReader(encoded))
	if err != nil || decoded != request {
		t.Errorf("decoded %+v, %v, want %+v", decoded, err, request)
	}
}

func TestSeedInTheAddonsIsAcceptedAndIgnored(t *testing.T) {
	input := []byte{0x00}
	input = append(input, sequentialUUID[:]...)
	input = append(input, 8, 0x12, 3, 1, 2, 3, 0x0A, 1, 'v')
	input = append(input, wire.CommandTCP, 0x00, 0x50, 0x01, 192, 0, 2, 1)

	request, err := wire.ReadRequestHeader(bytes.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if request.Flow != "v" {
		t.Errorf("Flow = %q, want it read past the seed", request.Flow)
	}
}

func TestReadRequestHeaderRefusesWhatItCannotRead(t *testing.T) {
	prefix := append([]byte{0x00}, sequentialUUID[:]...)
	with := func(rest ...byte) []byte { return append(append([]byte{}, prefix...), rest...) }

	tests := []struct {
		name  string
		input []byte
		want  error
	}{
		{"unknown version", append([]byte{0x01}, append(sequentialUUID[:], 0x00, wire.CommandTCP)...), wire.ErrUnknownVersion},
		{"unknown command", with(0x00, 0x09, 0x00, 0x50, 0x01, 1, 2, 3, 4), wire.ErrUnknownCommand},
		{"unknown address type", with(0x00, wire.CommandTCP, 0x00, 0x50, 0x04, 1, 2, 3, 4), wire.ErrUnsupportedAddressType},
		{"address type zero", with(0x00, wire.CommandTCP, 0x00, 0x50, 0x00), wire.ErrUnsupportedAddressType},
		{"empty domain", with(0x00, wire.CommandTCP, 0x00, 0x50, 0x02, 0x00), wire.ErrEmptyDomain},
		{"unknown addons field", with(0x02, 0x1A, 0x00, wire.CommandTCP), wire.ErrMalformedAddons},
		{"addons field longer than the addons", with(0x02, 0x0A, 0x09, wire.CommandTCP), wire.ErrMalformedAddons},
		{"addons ending inside a field header", with(0x01, 0x80, wire.CommandTCP), wire.ErrMalformedAddons},
		{"empty", nil, io.EOF},
		{"short prefix", prefix[:10], io.ErrUnexpectedEOF},
		{"no command", with(0x00), io.EOF},
		{"short address", with(0x00, wire.CommandTCP, 0x00, 0x50, 0x01, 1, 2), io.ErrUnexpectedEOF},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := wire.ReadRequestHeader(bytes.NewReader(test.input))
			if !errors.Is(err, test.want) {
				t.Errorf("error = %v, want one wrapping %v", err, test.want)
			}
		})
	}
}

func TestReadRequestHeaderWorksOneByteAtATime(t *testing.T) {
	encoded, err := wire.AppendRequestHeader(nil, wire.Request{UUID: sequentialUUID, Command: wire.CommandUDP, Destination: domainDestination(t, "dns.example", 53)})
	if err != nil {
		t.Fatal(err)
	}
	request, err := wire.ReadRequestHeader(iotest.OneByteReader(bytes.NewReader(encoded)))
	if err != nil || request.Command != wire.CommandUDP {
		t.Errorf("ReadRequestHeader = %+v, %v", request, err)
	}
}

func TestAppendRequestHeaderRefusesWhatTheFormatCannotCarry(t *testing.T) {
	longDomain := strings.Repeat("a", wire.MaxDomainLength+1)
	tests := []struct {
		name    string
		request wire.Request
		want    error
	}{
		{"mux with a destination", wire.Request{Command: wire.CommandMux, Destination: ipDestination(t, "192.0.2.1", 1)}, wire.ErrMuxNamesNoDestination},
		{"reverse", wire.Request{Command: wire.CommandReverse}, wire.ErrUnknownCommand},
		{"unknown command", wire.Request{Command: 9, Destination: ipDestination(t, "192.0.2.1", 1)}, wire.ErrUnknownCommand},
		{"domain too long", wire.Request{Command: wire.CommandTCP, Destination: domainDestination(t, longDomain, 1)}, wire.ErrDomainTooLong},
		{"flow too long for the length byte", wire.Request{Command: wire.CommandTCP, Flow: strings.Repeat("f", 300), Destination: ipDestination(t, "192.0.2.1", 1)}, wire.ErrMalformedAddons},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := wire.AppendRequestHeader(nil, test.request); !errors.Is(err, test.want) {
				t.Errorf("error = %v, want one wrapping %v", err, test.want)
			}
		})
	}
	if _, err := wire.AppendRequestHeader(nil, wire.Request{Command: wire.CommandTCP}); err == nil {
		t.Error("a request with no destination was encoded")
	}
}

func TestCommandOffsetSkipsTheAddons(t *testing.T) {
	encoded, err := wire.AppendRequestHeader(nil, wire.Request{
		UUID: sequentialUUID, Flow: "f", Command: wire.CommandUDP, Destination: ipDestination(t, "192.0.2.1", 80),
	})
	if err != nil {
		t.Fatal(err)
	}
	offset, err := wire.CommandOffset(encoded[:wire.RequestPrefixLength])
	if err != nil || encoded[offset] != wire.CommandUDP {
		t.Errorf("CommandOffset = %d, %v; byte there = X'%02X', want the command", offset, err, encoded[offset])
	}
	if _, err := wire.CommandOffset(encoded[:wire.RequestPrefixLength-1]); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("a short prefix gave %v, want io.ErrUnexpectedEOF", err)
	}
}

func TestResponseHeaderRoundTripsAndSkipsAddons(t *testing.T) {
	if got := wire.AppendResponseHeader(nil); !bytes.Equal(got, []byte{0x00, 0x00}) {
		t.Errorf("header = % X, want 00 00", got)
	}

	reader := bytes.NewReader([]byte{0x00, 0x03, 0xAA, 0xBB, 0xCC, 'p'})
	if err := wire.ReadResponseHeader(reader); err != nil {
		t.Fatal(err)
	}
	if rest, _ := io.ReadAll(reader); string(rest) != "p" {
		t.Errorf("left %q, want the payload after the addons", rest)
	}
}

func TestResponseHeaderRefusesAnotherVersionAndATruncatedHeader(t *testing.T) {
	if err := wire.ReadResponseHeader(bytes.NewReader([]byte{0x01, 0x00})); !errors.Is(err, wire.ErrUnknownVersion) {
		t.Errorf("version 1: error = %v, want ErrUnknownVersion", err)
	}
	if err := wire.ReadResponseHeader(bytes.NewReader([]byte{0x00, 0x04, 1})); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("truncated addons: error = %v, want io.ErrUnexpectedEOF", err)
	}
	if err := wire.ReadResponseHeader(bytes.NewReader(nil)); !errors.Is(err, io.EOF) {
		t.Errorf("empty: error = %v, want io.EOF", err)
	}
}

func TestUDPFrameMatchesHandWrittenBytes(t *testing.T) {
	got, err := wire.AppendUDPFrame(nil, []byte("abc"))
	if err != nil || !bytes.Equal(got, []byte{0x00, 0x03, 'a', 'b', 'c'}) {
		t.Errorf("frame = % X, %v, want 00 03 'abc'", got, err)
	}
}

func TestUDPFramesRoundTripBackToBackIncludingAnEmptyOne(t *testing.T) {
	var stream []byte
	for _, payload := range []string{"first", "", "third"} {
		var err error
		if stream, err = wire.AppendUDPFrame(stream, []byte(payload)); err != nil {
			t.Fatal(err)
		}
	}
	reader := bufio.NewReader(iotest.OneByteReader(bytes.NewReader(stream)))
	buffer := make([]byte, 16)
	for _, want := range []string{"first", "", "third"} {
		n, err := wire.ReadUDPFrame(reader, buffer)
		if err != nil || string(buffer[:n]) != want {
			t.Fatalf("frame = %q, %v, want %q", buffer[:n], err, want)
		}
	}
	if _, err := wire.ReadUDPFrame(reader, buffer); !errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("after the last frame: error = %v, want a clean io.EOF", err)
	}
}

func TestAFrameLargerThanTheBufferIsSkippedAndTheNextFrameStillReads(t *testing.T) {
	stream, _ := wire.AppendUDPFrame(nil, bytes.Repeat([]byte{'x'}, 100))
	stream, _ = wire.AppendUDPFrame(stream, []byte("next"))
	reader := bufio.NewReader(bytes.NewReader(stream))

	buffer := make([]byte, 16)
	if _, err := wire.ReadUDPFrame(reader, buffer); !errors.Is(err, spec.ErrMessageTooLong) {
		t.Fatalf("error = %v, want spec.ErrMessageTooLong", err)
	}
	n, err := wire.ReadUDPFrame(reader, buffer)
	if err != nil || string(buffer[:n]) != "next" {
		t.Errorf("frame after the long one = %q, %v, want it intact", buffer[:n], err)
	}
}

func TestAFrameCutShortIsAnUnexpectedEOF(t *testing.T) {
	for _, stream := range [][]byte{{0x00}, {0x00, 0x05, 'a'}} {
		_, err := wire.ReadUDPFrame(bufio.NewReader(bytes.NewReader(stream)), make([]byte, 16))
		if !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Errorf("% X: error = %v, want io.ErrUnexpectedEOF", stream, err)
		}
	}
}

func TestAppendUDPFrameRefusesAPayloadTheLengthFieldCannotExpress(t *testing.T) {
	if _, err := wire.AppendUDPFrame(nil, make([]byte, wire.MaxUDPPayload+1)); !errors.Is(err, spec.ErrMessageTooLong) {
		t.Errorf("error = %v, want spec.ErrMessageTooLong", err)
	}
	if _, err := wire.AppendUDPFrame(nil, make([]byte, wire.MaxUDPPayload)); err != nil {
		t.Errorf("the largest payload was refused: %v", err)
	}
}

// Checked against uuid.uuid5(uuid.UUID(int=0), text) in Python, which is what
// Xray (common/uuid) and sing-box (uuid.NewV5(uuid.Nil, text)) compute.
func TestParseUUIDMapsAnyShortStringTheWayXrayAndSingBoxDo(t *testing.T) {
	tests := map[string]string{
		"example": "feb54431-301b-52bb-a6dd-e1e93e81bb9e",
		"a":       "35b65f33-a679-5e76-af3c-273ea349ede4",
	}
	for text, canonical := range tests {
		derived, err := wire.ParseUUID(text)
		if err != nil {
			t.Fatalf("%q: %v", text, err)
		}
		want, err := wire.ParseUUID(canonical)
		if err != nil {
			t.Fatal(err)
		}
		if derived != want {
			t.Errorf("ParseUUID(%q) is a different UUID than %s", text, canonical)
		}
	}
}

func TestParseUUIDReadsTheCanonicalFormWithAndWithoutDashes(t *testing.T) {
	want := wire.UUID{0xb8, 0x31, 0x38, 0x1d, 0x63, 0x24, 0x4d, 0x53, 0xad, 0x4f, 0x8c, 0xda, 0x48, 0xb3, 0x08, 0x11}
	for _, text := range []string{
		"b831381d-6324-4d53-ad4f-8cda48b30811",
		"b831381d63244d53ad4f8cda48b30811",
		"B831381D-6324-4D53-AD4F-8CDA48B30811",
	} {
		parsed, err := wire.ParseUUID(text)
		if err != nil || parsed != want {
			t.Errorf("ParseUUID(%q) gave the expected UUID = %v, error %v, want the same UUID in every spelling", text, parsed == want, err)
		}
	}
}

func TestParseUUIDRefusesWhatXrayRefuses(t *testing.T) {
	for _, text := range []string{
		"",
		strings.Repeat("a", 31),
		strings.Repeat("a", 37),
		"b831381d-6324-4d53-ad4f-8cda48b3081g",
		"b831381d6324-4d53-ad4f-8cda48b30811",
		"b831381d63244d53ad4f8cda48b308-1",
		"zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz",
		"b831381d-6324-4d53-ad4f-8cda48b3081",
	} {
		if _, err := wire.ParseUUID(text); !errors.Is(err, wire.ErrInvalidUUID) {
			t.Errorf("ParseUUID(%q): error = %v, want ErrInvalidUUID", text, err)
		}
	}
	if _, err := wire.ParseUUID(strings.Repeat("a", 30)); err != nil {
		t.Errorf("30 bytes was refused: %v", err)
	}
}

func TestAUUIDNeverAppearsInFormattedOutput(t *testing.T) {
	for _, verb := range []string{"%v", "%+v", "%s", "%q", "%x", "%#v"} {
		text := fmt.Sprintf(verb, sequentialUUID)
		if strings.Contains(text, "0001") || strings.Contains(text, "\x01") {
			t.Errorf("%s printed %q, which holds the UUID", verb, text)
		}
	}
	text := fmt.Sprintf("%v", wire.Request{UUID: sequentialUUID})
	if !strings.Contains(text, "[redacted]") {
		t.Errorf("a request printed as %q, want the UUID redacted", text)
	}
}
