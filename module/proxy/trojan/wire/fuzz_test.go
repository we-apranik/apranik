package wire_test

import (
	"bytes"
	"testing"

	"github.com/The-Apranik/Apranik/module/proxy/trojan/wire"
)

// Anything ReadRequest accepts re-encodes to the bytes it consumed, and
// RequestLength agrees on where the header ends. Never panics.
func FuzzReadRequest(f *testing.F) {
	f.Add(validHeader())
	f.Add([]byte{})
	f.Add(bytes.Repeat([]byte{'a'}, wire.PasswordHashLength+4))

	f.Fuzz(func(t *testing.T, input []byte) {
		reader := bytes.NewReader(input)
		request, err := wire.ReadRequest(reader)
		if err != nil {
			return
		}
		consumed := input[:len(input)-reader.Len()]

		encoded, err := wire.AppendRequest(nil, request)
		if err != nil {
			t.Fatalf("accepted a request it cannot encode: %v", err)
		}
		if !bytes.Equal(encoded, consumed) {
			t.Fatalf("re-encoded % X, consumed % X", encoded, consumed)
		}
		if length, err := wire.RequestLength(input); err != nil || length != len(consumed) {
			t.Fatalf("RequestLength = %d, %v; ReadRequest consumed %d", length, err, len(consumed))
		}
	})
}

func FuzzReadUDPFrame(f *testing.F) {
	f.Add([]byte{0x01, 8, 8, 8, 8, 0x00, 0x35, 0x00, 0x03, '\r', '\n', 'a', 'b', 'c'})
	f.Add([]byte{0x03, 1, 'a', 0, 53, 0, 0, '\r', '\n'})
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, input []byte) {
		buffer := make([]byte, wire.MaxUDPPayload)
		reader := bytes.NewReader(input)
		n, from, err := wire.ReadUDPFrame(reader, buffer)
		if err != nil {
			return
		}
		consumed := input[:len(input)-reader.Len()]

		encoded, err := wire.AppendUDPFrame(nil, from, buffer[:n])
		if err != nil {
			t.Fatalf("accepted a frame it cannot encode: %v", err)
		}
		if !bytes.Equal(encoded, consumed) {
			t.Fatalf("re-encoded % X, consumed % X", encoded, consumed)
		}
	})
}
