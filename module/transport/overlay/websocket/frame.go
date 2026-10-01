package websocket

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// RFC 6455 section 5.2.
const (
	opcodeContinuation byte = 0x0
	opcodeText         byte = 0x1
	opcodeBinary       byte = 0x2
	opcodeClose        byte = 0x8
	opcodePing         byte = 0x9
	opcodePong         byte = 0xA

	finalBit     byte = 0x80
	reservedBits byte = 0x70
	opcodeBits   byte = 0x0F
	maskBit      byte = 0x80

	maxControlPayloadLength = 125
	maxFrameHeaderLength    = 14
)

var (
	errReservedBitsSet       = errors.New("websocket: frame has reserved bits set")
	errUnknownOpcode         = errors.New("websocket: frame has an unknown opcode")
	errFragmentedControl     = errors.New("websocket: control frame is fragmented")
	errControlPayloadTooLong = errors.New("websocket: control frame payload is longer than 125 bytes")
	errFrameLengthTooLarge   = errors.New("websocket: frame length has its most significant bit set")
)

type frameHeader struct {
	final   bool
	opcode  byte
	masked  bool
	maskKey [4]byte
	length  uint64
}

func (h frameHeader) isControl() bool { return h.opcode&0x8 != 0 }

// Reads through Peek and Discard rather than into local arrays: an array
// passed to io.ReadFull escapes to the heap, which cost an allocation per
// frame.
func readFrameHeader(reader *bufio.Reader) (frameHeader, error) {
	fixed, err := readExactly(reader, 2)
	if err != nil {
		return frameHeader{}, err
	}
	header := frameHeader{
		final:  fixed[0]&finalBit != 0,
		opcode: fixed[0] & opcodeBits,
		masked: fixed[1]&maskBit != 0,
		length: uint64(fixed[1] &^ maskBit),
	}
	if fixed[0]&reservedBits != 0 {
		return frameHeader{}, errReservedBitsSet
	}
	switch header.opcode {
	case opcodeContinuation, opcodeText, opcodeBinary, opcodeClose, opcodePing, opcodePong:
	default:
		return frameHeader{}, fmt.Errorf("%w: 0x%x", errUnknownOpcode, header.opcode)
	}

	switch header.length {
	case 126:
		extended, err := readExactly(reader, 2)
		if err != nil {
			return frameHeader{}, err
		}
		header.length = uint64(binary.BigEndian.Uint16(extended))
	case 127:
		extended, err := readExactly(reader, 8)
		if err != nil {
			return frameHeader{}, err
		}
		header.length = binary.BigEndian.Uint64(extended)
		if header.length>>63 != 0 {
			return frameHeader{}, errFrameLengthTooLarge
		}
	}

	if header.isControl() {
		if !header.final {
			return frameHeader{}, errFragmentedControl
		}
		if header.length > maxControlPayloadLength {
			return frameHeader{}, errControlPayloadTooLong
		}
	}

	if header.masked {
		key, err := readExactly(reader, 4)
		if err != nil {
			return frameHeader{}, err
		}
		header.maskKey = [4]byte(key)
	}
	return header, nil
}

// readExactly returns the next n bytes, valid until the next read. A stream
// that ends partway is io.ErrUnexpectedEOF; one that ends before is io.EOF.
func readExactly(reader *bufio.Reader, n int) ([]byte, error) {
	peeked, err := reader.Peek(n)
	if len(peeked) < n {
		if err == io.EOF && len(peeked) > 0 {
			err = io.ErrUnexpectedEOF
		}
		return nil, err
	}
	reader.Discard(n)
	return peeked, nil
}

// appendFrameHeader uses the shortest length encoding, as RFC 6455 requires.
func appendFrameHeader(destination []byte, header frameHeader) []byte {
	first := header.opcode
	if header.final {
		first |= finalBit
	}
	var maskFlag byte
	if header.masked {
		maskFlag = maskBit
	}
	switch {
	case header.length <= 125:
		destination = append(destination, first, maskFlag|byte(header.length))
	case header.length <= 0xFFFF:
		destination = append(destination, first, maskFlag|126)
		destination = binary.BigEndian.AppendUint16(destination, uint16(header.length))
	default:
		destination = append(destination, first, maskFlag|127)
		destination = binary.BigEndian.AppendUint64(destination, header.length)
	}
	if header.masked {
		destination = append(destination, header.maskKey[:]...)
	}
	return destination
}

// maskBytes XORs payload in place with the key, starting at keyOffset into
// the key, and returns the offset to continue from. Masking and unmasking are
// the same operation.
//
// Eight bytes at a time: the key repeats every four bytes, so a 64-bit word of
// it, rotated to keyOffset, masks eight at once and leaves the offset where it
// was.
func maskBytes(key [4]byte, keyOffset int, payload []byte) int {
	if len(payload) >= 8 {
		var rotated [8]byte
		for index := range rotated {
			rotated[index] = key[(keyOffset+index)&3]
		}
		word := binary.LittleEndian.Uint64(rotated[:])
		for len(payload) >= 8 {
			binary.LittleEndian.PutUint64(payload, binary.LittleEndian.Uint64(payload)^word)
			payload = payload[8:]
		}
	}
	for index := range payload {
		payload[index] ^= key[(keyOffset+index)&3]
	}
	return (keyOffset + len(payload)) & 3
}
