package muxcool_test

import (
	"bufio"
	"bytes"
	"testing"

	"github.com/The-Apranik/Apranik/shared/muxcool"
)

// Whatever the bytes, ReadHeader must not panic or read past the metadata, and
// a header it accepts must be writable and read back as the same header.
func FuzzReadHeader(f *testing.F) {
	for _, wire := range [][]byte{
		bytesOf(0x00, 0x14, 0x00, 0x01, 0x01, 0x00, 0x01, 0x00, 0x50, 0x02, 0x0B, "example.com"),
		bytesOf(0x00, 0x14, 0x00, 0x03, 0x01, 0x01, 0x02, 0x00, 0x35, 0x01, 8, 8, 8, 8, 1, 2, 3, 4, 5, 6, 7, 8),
		bytesOf(0x00, 0x0C, 0x00, 0x00, 0x02, 0x01, 0x02, 0x00, 0x35, 0x01, 1, 1, 1, 1),
		bytesOf(0x00, 0x04, 0x00, 0x01, 0x03, 0x02),
		bytesOf(0x00, 0x04, 0xBE, 0xEF, 0x04, 0x00),
		bytesOf(0x00, 0x03),
		bytesOf(0x02, 0x01),
	} {
		f.Add(wire)
	}

	f.Fuzz(func(t *testing.T, wire []byte) {
		header, err := muxcool.ReadHeader(bytes.NewReader(wire))
		buffered, bufferedErr := muxcool.ReadHeader(bufio.NewReaderSize(bytes.NewReader(wire), 4096))
		if (err == nil) != (bufferedErr == nil) || (err == nil && header != buffered) {
			t.Fatalf("plain reader: %+v, %v; buffered reader: %+v, %v", header, err, buffered, bufferedErr)
		}
		if err != nil {
			return
		}

		header.Options &= muxcool.OptionDataFollows | muxcool.OptionError // unknown bits are tolerated on read, never written
		written, err := muxcool.AppendHeader(nil, header)
		if err != nil {
			t.Fatalf("a header ReadHeader accepted, %+v, cannot be written: %v", header, err)
		}
		again, err := muxcool.ReadHeader(bytes.NewReader(written))
		if err != nil || again != header {
			t.Fatalf("round trip of %+v gave %+v, %v", header, again, err)
		}
	})
}
