package wire_test

import (
	"bufio"
	"bytes"
	"testing"

	"github.com/The-Apranik/Apranik/module/proxy/vless/wire"
)

// Anything ReadRequest accepts as a TCP or UDP request encodes and reads back
// to the same request, and CommandOffset finds its command byte. Never panics.
func FuzzReadRequest(f *testing.F) {
	valid, _ := wire.AppendRequest(nil, wire.Request{UUID: sequentialUUID, Command: wire.CommandTCP, Destination: domainDestination(f, "example.com", 443)})
	f.Add(valid)
	f.Add([]byte{})
	f.Add(append([]byte{0}, make([]byte, 18)...))

	f.Fuzz(func(t *testing.T, input []byte) {
		reader := bytes.NewReader(input)
		request, err := wire.ReadRequest(reader)
		if err != nil {
			return
		}
		offset, err := wire.CommandOffset(input)
		if err != nil || input[offset] != request.Command {
			t.Fatalf("CommandOffset = %d, %v; the command is X'%02X'", offset, err, request.Command)
		}
		if request.Command != wire.CommandTCP && request.Command != wire.CommandUDP {
			return
		}

		encoded, err := wire.AppendRequest(nil, request)
		if err != nil {
			t.Fatalf("accepted a request it cannot encode: %v", err)
		}
		again, err := wire.ReadRequest(bytes.NewReader(encoded))
		if err != nil || again != request {
			t.Fatalf("re-read %+v, %v; want %+v", again, err, request)
		}
	})
}

func FuzzReadUDPFrame(f *testing.F) {
	f.Add([]byte{0x00, 0x03, 'a', 'b', 'c'})
	f.Add([]byte{0xFF, 0xFF})

	f.Fuzz(func(t *testing.T, input []byte) {
		reader := bufio.NewReader(bytes.NewReader(input))
		buffer := make([]byte, 64)
		for {
			n, err := wire.ReadUDPFrame(reader, buffer)
			if err != nil {
				return
			}
			if n > len(buffer) {
				t.Fatalf("read %d bytes into a %d byte buffer", n, len(buffer))
			}
		}
	})
}

func FuzzParseUUID(f *testing.F) {
	f.Add("example")
	f.Add("b831381d-6324-4d53-ad4f-8cda48b30811")

	f.Fuzz(func(t *testing.T, text string) {
		parsed, err := wire.ParseUUID(text)
		if err != nil {
			return
		}
		if again, err := wire.ParseUUID(text); err != nil || again != parsed {
			t.Fatalf("parsing %q twice gave different results", text)
		}
	})
}
