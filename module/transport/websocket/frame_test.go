package websocket

import (
	"bufio"
	"bytes"
	"errors"
	"testing"
)

func TestTheAcceptKeyMatchesRFC6455(t *testing.T) {
	if got := acceptKeyFor("dGhlIHNhbXBsZSBub25jZQ=="); got != "s3pPLMBiTxaQ9kYGzzhZRbK+xOo=" {
		t.Errorf("accept key %q", got)
	}
}

// The example frames of RFC 6455 section 5.7.
func TestTheRFCExampleFramesDecode(t *testing.T) {
	for name, tc := range map[string]struct {
		wire    []byte
		opcode  byte
		payload string
	}{
		"unmasked text": {[]byte{0x81, 0x05, 0x48, 0x65, 0x6c, 0x6c, 0x6f}, opcodeText, "Hello"},
		"masked text":   {[]byte{0x81, 0x85, 0x37, 0xfa, 0x21, 0x3d, 0x7f, 0x9f, 0x4d, 0x51, 0x58}, opcodeText, "Hello"},
		"ping":          {[]byte{0x89, 0x05, 0x48, 0x65, 0x6c, 0x6c, 0x6f}, opcodePing, "Hello"},
		"masked pong":   {[]byte{0x8a, 0x85, 0x37, 0xfa, 0x21, 0x3d, 0x7f, 0x9f, 0x4d, 0x51, 0x58}, opcodePong, "Hello"},
	} {
		reader := bufio.NewReader(bytes.NewReader(tc.wire))
		header, err := readFrameHeader(reader)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		payload := make([]byte, header.length)
		reader.Read(payload)
		if header.masked {
			maskBytes(header.maskKey, 0, payload)
		}
		if header.opcode != tc.opcode || !header.final || string(payload) != tc.payload {
			t.Errorf("%s: opcode %x final %v payload %q", name, header.opcode, header.final, payload)
		}
	}
}

func TestLengthsUseTheShortestEncodingAndRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		length       uint64
		headerLength int
	}{{0, 2}, {125, 2}, {126, 4}, {65535, 4}, {65536, 10}, {1 << 40, 10}} {
		for _, masked := range []bool{false, true} {
			written := frameHeader{final: true, opcode: opcodeBinary, masked: masked, length: tc.length}
			if masked {
				written.maskKey = [4]byte{1, 2, 3, 4}
			}
			encoded := appendFrameHeader(nil, written)
			want := tc.headerLength
			if masked {
				want += 4
			}
			if len(encoded) != want {
				t.Errorf("length %d masked %v: %d header bytes, want %d", tc.length, masked, len(encoded), want)
			}
			read, err := readFrameHeader(bufio.NewReader(bytes.NewReader(encoded)))
			if err != nil || read != written {
				t.Errorf("length %d masked %v: read %+v, %v", tc.length, masked, read, err)
			}
		}
	}
}

func TestMalformedFramesAreRefused(t *testing.T) {
	for name, tc := range map[string]struct {
		wire []byte
		want error
	}{
		"reserved bit":    {[]byte{0xC2, 0x00}, errReservedBitsSet},
		"unknown opcode":  {[]byte{0x83, 0x00}, errUnknownOpcode},
		"fragmented ping": {[]byte{0x09, 0x00}, errFragmentedControl},
		"long ping":       {[]byte{0x89, 0x7E, 0x00, 0x80}, errControlPayloadTooLong},
		"length top bit":  {[]byte{0x82, 0x7F, 0x80, 0, 0, 0, 0, 0, 0, 0}, errFrameLengthTooLarge},
	} {
		if _, err := readFrameHeader(bufio.NewReader(bytes.NewReader(tc.wire))); !errors.Is(err, tc.want) {
			t.Errorf("%s: %v, want %v", name, err, tc.want)
		}
	}
}

// Masking can stop and resume mid-key, as a Read that fills a small buffer does.
func TestMaskingResumesAcrossCalls(t *testing.T) {
	key := [4]byte{0xde, 0xad, 0xbe, 0xef}
	original := []byte("the quick brown fox jumps over")
	whole := bytes.Clone(original)
	maskBytes(key, 0, whole)

	pieces := bytes.Clone(original)
	offset := 0
	for start := 0; start < len(pieces); start += 7 {
		end := min(start+7, len(pieces))
		offset = maskBytes(key, offset, pieces[start:end])
	}
	if !bytes.Equal(whole, pieces) {
		t.Error("masking in pieces differs from masking at once")
	}
}

func FuzzReadFrameHeader(f *testing.F) {
	f.Add([]byte{0x81, 0x85, 0x37, 0xfa, 0x21, 0x3d, 0x7f, 0x9f, 0x4d, 0x51, 0x58})
	f.Add([]byte{0x82, 0x7F, 0, 0, 0, 0, 0, 1, 0, 0})
	f.Fuzz(func(t *testing.T, wire []byte) {
		header, err := readFrameHeader(bufio.NewReader(bytes.NewReader(wire)))
		if err != nil {
			return
		}
		if header.isControl() && (!header.final || header.length > maxControlPayloadLength) {
			t.Fatalf("accepted a bad control frame %+v", header)
		}
		reencoded := appendFrameHeader(nil, header)
		again, err := readFrameHeader(bufio.NewReader(bytes.NewReader(reencoded)))
		if err != nil || again != header {
			t.Fatalf("re-encoded header %+v read back as %+v, %v", header, again, err)
		}
	})
}
