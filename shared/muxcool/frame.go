package muxcool

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/The-Apranik/Apranik/shared/vmessaddr"
	"github.com/The-Apranik/Apranik/spec"
)

type Status byte

const (
	StatusNew       Status = 1
	StatusKeep      Status = 2
	StatusEnd       Status = 3
	StatusKeepAlive Status = 4
)

func (s Status) String() string {
	switch s {
	case StatusNew:
		return "New"
	case StatusKeep:
		return "Keep"
	case StatusEnd:
		return "End"
	case StatusKeepAlive:
		return "KeepAlive"
	default:
		return fmt.Sprintf("Status(%d)", byte(s))
	}
}

type Network byte

const (
	NetworkTCP Network = 1
	NetworkUDP Network = 2
)

func (n Network) String() string {
	switch n {
	case NetworkTCP:
		return "TCP"
	case NetworkUDP:
		return "UDP"
	default:
		return fmt.Sprintf("Network(%d)", byte(n))
	}
}

type FrameOptions byte

const (
	OptionDataFollows FrameOptions = 1
	OptionError       FrameOptions = 2

	knownOptions = OptionDataFollows | OptionError
)

func (o FrameOptions) Has(option FrameOptions) bool { return o&option == option }

const (
	// MaxMetadataLength is Xray's limit; the longest metadata this package
	// writes is 264 bytes (a 255-byte domain).
	MaxMetadataLength = 512

	// MaxDataLength is what the two-byte data length can express.
	MaxDataLength = 0xFFFF

	minMetadataLength = 4 // ID, status, options
)

// Network and Destination are set on New, and on a Keep that names an address.
// GlobalID is the XUDP session ID, which only a New frame of a UDP
// sub-connection with data can carry; zero means none.
type Header struct {
	SubConnectionID uint16
	Status          Status
	Options         FrameOptions
	Network         Network
	Destination     spec.Destination
	GlobalID        [8]byte
}

func AppendHeader(buffer []byte, header Header) ([]byte, error) {
	if header.Options&^knownOptions != 0 {
		return nil, fmt.Errorf("muxcool: options %#x include bits this format does not define", byte(header.Options))
	}

	start := len(buffer)
	buffer = append(buffer, 0, 0) // the length, filled in below
	buffer = binary.BigEndian.AppendUint16(buffer, header.SubConnectionID)
	buffer = append(buffer, byte(header.Status), byte(header.Options))

	switch header.Status {
	case StatusNew:
		if header.Network != NetworkTCP && header.Network != NetworkUDP {
			return nil, fmt.Errorf("muxcool: a New frame needs the network TCP or UDP, got %s", header.Network)
		}
		if !header.Destination.IsValid() {
			return nil, errors.New("muxcool: a New frame needs a destination")
		}
	case StatusKeep:
		switch header.Network {
		case 0:
			if header.Destination.IsValid() {
				return nil, errors.New("muxcool: a Keep frame names a destination only with the network UDP")
			}
		case NetworkUDP:
			if !header.Destination.IsValid() {
				return nil, errors.New("muxcool: a UDP Keep frame that names the network needs a destination")
			}
		default:
			return nil, fmt.Errorf("muxcool: a Keep frame carries no address for the network %s", header.Network)
		}
	case StatusEnd, StatusKeepAlive:
		if header.Network != 0 || header.Destination.IsValid() {
			return nil, fmt.Errorf("muxcool: a %s frame carries no network and no destination", header.Status)
		}
	default:
		return nil, fmt.Errorf("muxcool: cannot write the frame status %s", header.Status)
	}

	if header.Network != 0 {
		buffer = append(buffer, byte(header.Network))
		var err error
		if buffer, err = vmessaddr.AppendAddress(buffer, header.Destination); err != nil {
			return nil, err
		}
	}

	if header.GlobalID != [8]byte{} {
		if !carriesGlobalID(header.Status, header.Network, header.Options) {
			return nil, errors.New("muxcool: only a New frame of a UDP sub-connection with data carries a Global ID")
		}
		buffer = append(buffer, header.GlobalID[:]...)
	}

	binary.BigEndian.PutUint16(buffer[start:], uint16(len(buffer)-start-2))
	return buffer, nil
}

func carriesGlobalID(status Status, network Network, options FrameOptions) bool {
	return status == StatusNew && network == NetworkUDP && options.Has(OptionDataFollows)
}

// ReadHeader reads one frame's length and metadata and nothing after it, so
// the data that may follow stays in reader. A reader that ends before the first
// byte gives io.EOF, the clean end of a main connection; one that ends inside
// the metadata gives io.ErrUnexpectedEOF. Everything the format does not allow
// is an error that wraps ErrProtocol, including an unknown status.
//
// A New frame of a UDP sub-connection with data has its Global ID read from the
// eight bytes after the address, when they are there. Other bytes after the
// fields the status defines are skipped: they are padding.
//
// A *bufio.Reader of at least 514 bytes is read in place, with no allocation
// for a frame without a domain; the engine reads its carrier through one.
func ReadHeader(reader io.Reader) (Header, error) {
	if buffered, ok := reader.(*bufio.Reader); ok && buffered.Size() >= 2+MaxMetadataLength {
		return readHeaderInPlace(buffered)
	}

	var length [2]byte
	if _, err := io.ReadFull(reader, length[:]); err != nil {
		return Header{}, fmt.Errorf("muxcool: reading metadata length: %w", err)
	}
	size, err := checkMetadataLength(length[:])
	if err != nil {
		return Header{}, err
	}
	metadata := make([]byte, size)
	if _, err := io.ReadFull(reader, metadata); err != nil {
		return Header{}, fmt.Errorf("muxcool: reading metadata: %w", unexpectedIfEnded(err))
	}
	return parseMetadata(metadata)
}

func readHeaderInPlace(reader *bufio.Reader) (Header, error) {
	length, err := reader.Peek(2)
	if err != nil {
		if len(length) == 1 {
			err = unexpectedIfEnded(err)
		}
		return Header{}, fmt.Errorf("muxcool: reading metadata length: %w", err)
	}
	size, err := checkMetadataLength(length)
	if err != nil {
		return Header{}, err
	}
	frame, err := reader.Peek(2 + size)
	if err != nil {
		return Header{}, fmt.Errorf("muxcool: reading metadata: %w", unexpectedIfEnded(err))
	}
	header, err := parseMetadata(frame[2:])
	if err != nil {
		return Header{}, err
	}
	_, _ = reader.Discard(2 + size)
	return header, nil
}

func checkMetadataLength(length []byte) (int, error) {
	size := int(binary.BigEndian.Uint16(length))
	if size < minMetadataLength || size > MaxMetadataLength {
		return 0, fmt.Errorf("%w: metadata length %d, want %d to %d", ErrProtocol, size, minMetadataLength, MaxMetadataLength)
	}
	return size, nil
}

func parseMetadata(metadata []byte) (Header, error) {
	header := Header{
		SubConnectionID: binary.BigEndian.Uint16(metadata),
		Status:          Status(metadata[2]),
		Options:         FrameOptions(metadata[3]),
	}
	extra := metadata[minMetadataLength:]

	switch header.Status {
	case StatusNew:
		if len(extra) == 0 {
			return Header{}, fmt.Errorf("%w: a New frame without a network and a destination", ErrProtocol)
		}
		header.Network = Network(extra[0])
		if header.Network != NetworkTCP && header.Network != NetworkUDP {
			return Header{}, fmt.Errorf("%w: a New frame for the network %d", ErrProtocol, extra[0])
		}
		destination, addressLength, err := vmessaddr.ParseAddress(extra[1:])
		if err != nil {
			return Header{}, fmt.Errorf("%w: New frame destination: %w", ErrProtocol, err)
		}
		header.Destination = destination
		// Xray reads the ID when New, data and UDP are all there and eight bytes remain.
		if afterAddress := extra[1+addressLength:]; carriesGlobalID(header.Status, header.Network, header.Options) && len(afterAddress) >= len(header.GlobalID) {
			copy(header.GlobalID[:], afterAddress)
		}
	case StatusKeep:
		// Only a UDP Keep names an address; Xray reads it by the network byte,
		// not by what the sub-connection was opened for.
		if len(extra) > 0 && Network(extra[0]) == NetworkUDP {
			header.Network = NetworkUDP
			var err error
			if header.Destination, _, err = vmessaddr.ParseAddress(extra[1:]); err != nil {
				return Header{}, fmt.Errorf("%w: Keep frame destination: %w", ErrProtocol, err)
			}
		}
	case StatusEnd, StatusKeepAlive:
	default:
		return Header{}, fmt.Errorf("%w: unknown frame status %d", ErrProtocol, metadata[2])
	}
	return header, nil
}

func AppendDataLength(buffer []byte, length int) ([]byte, error) {
	if length < 0 || length > MaxDataLength {
		return nil, fmt.Errorf("muxcool: data of %d bytes does not fit a frame (at most %d)", length, MaxDataLength)
	}
	return binary.BigEndian.AppendUint16(buffer, uint16(length)), nil
}

func ReadDataLength(reader io.Reader) (int, error) {
	var length [2]byte
	if _, err := io.ReadFull(reader, length[:]); err != nil {
		return 0, fmt.Errorf("muxcool: reading data length: %w", unexpectedIfEnded(err))
	}
	return int(binary.BigEndian.Uint16(length[:])), nil
}

func unexpectedIfEnded(err error) error {
	if errors.Is(err, io.EOF) {
		return io.ErrUnexpectedEOF
	}
	return err
}
