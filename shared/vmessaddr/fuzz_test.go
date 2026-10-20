package vmessaddr_test

import (
	"bytes"
	"testing"

	"github.com/we-apranik/apranik/shared/vmessaddr"
)

// Whatever the bytes, the two readers must not panic, must agree, and what they
// accept must be written back to exactly the bytes they consumed.
func FuzzReadAndParseAddress(f *testing.F) {
	f.Add([]byte{0x00, 0x50, 0x01, 1, 2, 3, 4})
	f.Add(append([]byte{0x1F, 0x90, 0x02, 11}, "example.com"...))
	f.Add([]byte{0x01, 0xBB, 0x03, 0x20, 0x01, 0x0D, 0xB8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1})
	f.Add([]byte{0x00, 0x50, 0x02, 0})
	f.Add([]byte{0x00, 0x50, 0x09})

	f.Fuzz(func(t *testing.T, encoded []byte) {
		parsed, length, parseErr := vmessaddr.ParseAddress(encoded)
		reader := bytes.NewReader(encoded)
		read, readErr := vmessaddr.ReadAddress(reader)

		if (parseErr == nil) != (readErr == nil) {
			t.Fatalf("ParseAddress error %v, ReadAddress error %v", parseErr, readErr)
		}
		if parseErr != nil {
			return
		}
		if parsed != read {
			t.Fatalf("ParseAddress = %s, ReadAddress = %s", parsed, read)
		}
		if consumed := len(encoded) - reader.Len(); consumed != length {
			t.Fatalf("ParseAddress took %d bytes, ReadAddress %d", length, consumed)
		}
		again, err := vmessaddr.AppendAddress(nil, parsed)
		if err != nil {
			t.Fatal(err)
		}
		reparsed, _, err := vmessaddr.ParseAddress(again)
		if err != nil || reparsed != parsed {
			t.Fatalf("round trip of %s gave %s, %v", parsed, reparsed, err)
		}
	})
}
