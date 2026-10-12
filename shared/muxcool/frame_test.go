package muxcool_test

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"net/netip"
	"strings"
	"testing"

	"github.com/The-Apranik/Apranik/shared/muxcool"
	"github.com/The-Apranik/Apranik/shared/vmessaddr"
	"github.com/The-Apranik/Apranik/spec"
)

func ipDestination(t testing.TB, text string, port uint16) spec.Destination {
	t.Helper()
	destination, err := spec.NewDestinationFromIP(netip.MustParseAddr(text), port)
	if err != nil {
		t.Fatal(err)
	}
	return destination
}

func domainDestination(t testing.TB, domain string, port uint16) spec.Destination {
	t.Helper()
	destination, err := spec.NewDestinationFromDomain(domain, port)
	if err != nil {
		t.Fatal(err)
	}
	return destination
}

func bytesOf(parts ...any) []byte {
	var out []byte
	for _, part := range parts {
		switch value := part.(type) {
		case string:
			out = append(out, value...)
		case []byte:
			out = append(out, value...)
		case int:
			out = append(out, byte(value))
		default:
			panic("bytesOf: unsupported part")
		}
	}
	return out
}

// Each case is a frame written by hand from the Mux.Cool specification (the
// Xray page), not produced by this package, so the encoder and the decoder are
// checked against the format and not only against each other.
func headerCases(t *testing.T) []struct {
	name   string
	header muxcool.Header
	wire   []byte
} {
	return []struct {
		name   string
		header muxcool.Header
		wire   []byte
	}{
		{
			"New TCP to a domain, no data",
			muxcool.Header{SubConnectionID: 1, Status: muxcool.StatusNew, Network: muxcool.NetworkTCP, Destination: domainDestination(t, "example.com", 80)},
			bytesOf(0x00, 0x14, 0x00, 0x01, 0x01, 0x00, 0x01, 0x00, 0x50, 0x02, 0x0B, "example.com"),
		},
		{
			"New UDP to an IPv4 address, with data",
			muxcool.Header{SubConnectionID: 3, Status: muxcool.StatusNew, Options: muxcool.OptionDataFollows, Network: muxcool.NetworkUDP, Destination: ipDestination(t, "8.8.8.8", 53)},
			bytesOf(0x00, 0x0C, 0x00, 0x03, 0x01, 0x01, 0x02, 0x00, 0x35, 0x01, 8, 8, 8, 8),
		},
		{
			"New TCP to an IPv6 address",
			muxcool.Header{SubConnectionID: 0x0102, Status: muxcool.StatusNew, Options: muxcool.OptionDataFollows, Network: muxcool.NetworkTCP, Destination: ipDestination(t, "2001:db8::1", 443)},
			bytesOf(0x00, 0x18, 0x01, 0x02, 0x01, 0x01, 0x01, 0x01, 0xBB, 0x03, 0x20, 0x01, 0x0D, 0xB8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1),
		},
		{
			"Keep of a TCP sub-connection, with data",
			muxcool.Header{SubConnectionID: 1, Status: muxcool.StatusKeep, Options: muxcool.OptionDataFollows},
			bytesOf(0x00, 0x04, 0x00, 0x01, 0x02, 0x01),
		},
		{
			"Keep of a UDP sub-connection naming an address (XUDP)",
			muxcool.Header{SubConnectionID: 0, Status: muxcool.StatusKeep, Options: muxcool.OptionDataFollows, Network: muxcool.NetworkUDP, Destination: ipDestination(t, "1.1.1.1", 53)},
			bytesOf(0x00, 0x0C, 0x00, 0x00, 0x02, 0x01, 0x02, 0x00, 0x35, 0x01, 1, 1, 1, 1),
		},
		{
			"End with the error bit",
			muxcool.Header{SubConnectionID: 1, Status: muxcool.StatusEnd, Options: muxcool.OptionError},
			bytesOf(0x00, 0x04, 0x00, 0x01, 0x03, 0x02),
		},
		{
			"End, nothing more",
			muxcool.Header{SubConnectionID: 65535, Status: muxcool.StatusEnd},
			bytesOf(0x00, 0x04, 0xFF, 0xFF, 0x03, 0x00),
		},
		{
			"KeepAlive with an arbitrary ID",
			muxcool.Header{SubConnectionID: 0xBEEF, Status: muxcool.StatusKeepAlive},
			bytesOf(0x00, 0x04, 0xBE, 0xEF, 0x04, 0x00),
		},
	}
}

func TestAppendHeaderWritesTheFramesTheSpecificationShows(t *testing.T) {
	for _, tc := range headerCases(t) {
		got, err := muxcool.AppendHeader(nil, tc.header)
		if err != nil || !bytes.Equal(got, tc.wire) {
			t.Errorf("%s: AppendHeader = % x, %v; want % x", tc.name, got, err, tc.wire)
		}
	}
}

// readers gives each test the two ways a header is read: from any reader, and in
// place from the bufio.Reader the engine uses. Both must behave the same.
func readers(wire []byte) map[string]io.Reader {
	return map[string]io.Reader{
		"plain reader":    bytes.NewReader(wire),
		"buffered reader": bufio.NewReaderSize(bytes.NewReader(wire), 4096),
	}
}

func TestReadHeaderReadsThemBackAndLeavesTheDataInTheReader(t *testing.T) {
	for _, tc := range headerCases(t) {
		for readerName, reader := range readers(append(bytes.Clone(tc.wire), "data"...)) {
			got, err := muxcool.ReadHeader(reader)
			if err != nil || got != tc.header {
				t.Errorf("%s, %s: ReadHeader = %+v, %v; want %+v", tc.name, readerName, got, err, tc.header)
			}
			if rest, _ := io.ReadAll(reader); string(rest) != "data" {
				t.Errorf("%s, %s: ReadHeader left %q, want the bytes after the metadata untouched", tc.name, readerName, rest)
			}
		}
	}
}

func TestAppendHeaderKeepsWhatIsAlreadyInTheBuffer(t *testing.T) {
	got, err := muxcool.AppendHeader([]byte{0xAA, 0xBB}, muxcool.Header{SubConnectionID: 1, Status: muxcool.StatusEnd})
	if err != nil || !bytes.Equal(got, bytesOf(0xAA, 0xBB, 0x00, 0x04, 0x00, 0x01, 0x03, 0x00)) {
		t.Errorf("AppendHeader = % x, %v", got, err)
	}
}

// Both cores skip what they do not know after the fields of a frame: sing-box
// discards the rest of the metadata, Xray treats it as padding or, after a New
// frame's address, as the XUDP Global ID.
func TestReadHeaderSkipsBytesAfterTheDefinedFields(t *testing.T) {
	globalID := bytesOf(1, 2, 3, 4, 5, 6, 7, 8)
	for _, tc := range []struct {
		name string
		wire []byte
		want muxcool.Header
	}{
		{
			"Global ID after a New address",
			bytesOf(0x00, 0x14, 0x00, 0x03, 0x01, 0x01, 0x02, 0x00, 0x35, 0x01, 8, 8, 8, 8, globalID),
			muxcool.Header{SubConnectionID: 3, Status: muxcool.StatusNew, Options: muxcool.OptionDataFollows, Network: muxcool.NetworkUDP, Destination: ipDestination(t, "8.8.8.8", 53)},
		},
		{
			"padding after End",
			bytesOf(0x00, 0x08, 0x00, 0x01, 0x03, 0x00, 0, 0, 0, 0),
			muxcool.Header{SubConnectionID: 1, Status: muxcool.StatusEnd},
		},
		{
			"padding after a TCP Keep",
			bytesOf(0x00, 0x06, 0x00, 0x01, 0x02, 0x01, 0x00, 0x00),
			muxcool.Header{SubConnectionID: 1, Status: muxcool.StatusKeep, Options: muxcool.OptionDataFollows},
		},
		{
			"padding after the address of a UDP Keep",
			bytesOf(0x00, 0x0E, 0x00, 0x00, 0x02, 0x01, 0x02, 0x00, 0x35, 0x01, 1, 1, 1, 1, 0, 0),
			muxcool.Header{SubConnectionID: 0, Status: muxcool.StatusKeep, Options: muxcool.OptionDataFollows, Network: muxcool.NetworkUDP, Destination: ipDestination(t, "1.1.1.1", 53)},
		},
		{
			"option bits this format does not define are kept as read",
			bytesOf(0x00, 0x04, 0x00, 0x01, 0x03, 0xFC),
			muxcool.Header{SubConnectionID: 1, Status: muxcool.StatusEnd, Options: 0xFC},
		},
	} {
		for readerName, reader := range readers(append(bytes.Clone(tc.wire), "data"...)) {
			got, err := muxcool.ReadHeader(reader)
			if err != nil || got != tc.want {
				t.Errorf("%s, %s: ReadHeader = %+v, %v; want %+v", tc.name, readerName, got, err, tc.want)
			}
			if rest, _ := io.ReadAll(reader); string(rest) != "data" {
				t.Errorf("%s, %s: left %q after the metadata", tc.name, readerName, rest)
			}
		}
	}
}

func TestReadHeaderAcceptsMetadataUpToTheLimitAndRefusesBeyondIt(t *testing.T) {
	domain := domainDestination(t, strings.Repeat("a", vmessaddr.MaxDomainLength), 80)
	base, err := muxcool.AppendHeader(nil, muxcool.Header{SubConnectionID: 1, Status: muxcool.StatusNew, Network: muxcool.NetworkTCP, Destination: domain})
	if err != nil {
		t.Fatal(err)
	}
	padTo := func(length int) []byte {
		frame := append(bytes.Clone(base), make([]byte, length-(len(base)-2))...)
		frame[0], frame[1] = byte(length>>8), byte(length)
		return frame
	}

	for readerName, reader := range readers(padTo(muxcool.MaxMetadataLength)) {
		if got, err := muxcool.ReadHeader(reader); err != nil || got.Destination != domain {
			t.Errorf("%s: metadata of %d bytes: %+v, %v", readerName, muxcool.MaxMetadataLength, got, err)
		}
	}
	for readerName, reader := range readers(padTo(muxcool.MaxMetadataLength + 1)) {
		if _, err := muxcool.ReadHeader(reader); !errors.Is(err, muxcool.ErrProtocol) {
			t.Errorf("%s: metadata of %d bytes: error = %v, want ErrProtocol", readerName, muxcool.MaxMetadataLength+1, err)
		}
	}
}

func TestReadHeaderRefusesWhatTheFormatDoesNotAllow(t *testing.T) {
	for _, tc := range []struct {
		name string
		wire []byte
		want error
	}{
		{"metadata shorter than ID, status and options", bytesOf(0x00, 0x03, 0x00, 0x01, 0x03), muxcool.ErrProtocol},
		{"metadata length zero", bytesOf(0x00, 0x00), muxcool.ErrProtocol},
		{"unknown status zero", bytesOf(0x00, 0x04, 0x00, 0x01, 0x00, 0x00), muxcool.ErrProtocol},
		{"unknown status five", bytesOf(0x00, 0x04, 0x00, 0x01, 0x05, 0x00), muxcool.ErrProtocol},
		{"New without a network", bytesOf(0x00, 0x04, 0x00, 0x01, 0x01, 0x00), muxcool.ErrProtocol},
		{"New for network zero", bytesOf(0x00, 0x0A, 0x00, 0x01, 0x01, 0x00, 0x00, 0x00, 0x50, 0x01, 1, 2, 3, 4), muxcool.ErrProtocol},
		{"New for network three", bytesOf(0x00, 0x0A, 0x00, 0x01, 0x01, 0x00, 0x03, 0x00, 0x50, 0x01, 1, 2, 3, 4), muxcool.ErrProtocol},
		{"New with an unknown address type", bytesOf(0x00, 0x0A, 0x00, 0x01, 0x01, 0x00, 0x01, 0x00, 0x50, 0x04, 1, 2, 3, 4), vmessaddr.ErrUnsupportedAddressType},
		{"New with an empty domain", bytesOf(0x00, 0x09, 0x00, 0x01, 0x01, 0x00, 0x01, 0x00, 0x50, 0x02, 0x00), vmessaddr.ErrEmptyDomain},
		{"New with a truncated address", bytesOf(0x00, 0x08, 0x00, 0x01, 0x01, 0x00, 0x01, 0x00, 0x50, 0x01), muxcool.ErrProtocol},
		{"UDP Keep with an unknown address type", bytesOf(0x00, 0x0A, 0x00, 0x01, 0x02, 0x01, 0x02, 0x00, 0x50, 0x07, 1, 2, 3, 4), vmessaddr.ErrUnsupportedAddressType},
	} {
		for readerName, reader := range readers(tc.wire) {
			if _, err := muxcool.ReadHeader(reader); !errors.Is(err, tc.want) {
				t.Errorf("%s, %s: error = %v, want %v", tc.name, readerName, err, tc.want)
			}
		}
	}
}

func TestReadHeaderTellsTheCleanEndFromATruncatedFrame(t *testing.T) {
	for readerName, reader := range readers(nil) {
		if _, err := muxcool.ReadHeader(reader); !errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			t.Errorf("%s, empty: error = %v, want io.EOF", readerName, err)
		}
	}
	for _, truncated := range [][]byte{
		bytesOf(0x00),
		bytesOf(0x00, 0x04),
		bytesOf(0x00, 0x04, 0x00, 0x01),
		bytesOf(0x00, 0x14, 0x00, 0x01, 0x01, 0x00, 0x01, 0x00, 0x50, 0x02, 0x0B, "example"),
	} {
		for readerName, reader := range readers(truncated) {
			if _, err := muxcool.ReadHeader(reader); !errors.Is(err, io.ErrUnexpectedEOF) {
				t.Errorf("%s, % x: error = %v, want io.ErrUnexpectedEOF", readerName, truncated, err)
			}
		}
	}
}

func TestAppendHeaderRefusesWhatTheFormatCannotCarry(t *testing.T) {
	destination := ipDestination(t, "1.2.3.4", 80)
	for _, tc := range []struct {
		name   string
		header muxcool.Header
	}{
		{"New without a network", muxcool.Header{Status: muxcool.StatusNew, Destination: destination}},
		{"New for network three", muxcool.Header{Status: muxcool.StatusNew, Network: 3, Destination: destination}},
		{"New without a destination", muxcool.Header{Status: muxcool.StatusNew, Network: muxcool.NetworkTCP}},
		{"Keep with a TCP network", muxcool.Header{Status: muxcool.StatusKeep, Network: muxcool.NetworkTCP, Destination: destination}},
		{"Keep with a destination and no network", muxcool.Header{Status: muxcool.StatusKeep, Destination: destination}},
		{"Keep naming UDP without a destination", muxcool.Header{Status: muxcool.StatusKeep, Network: muxcool.NetworkUDP}},
		{"End with a destination", muxcool.Header{Status: muxcool.StatusEnd, Network: muxcool.NetworkUDP, Destination: destination}},
		{"KeepAlive with a network", muxcool.Header{Status: muxcool.StatusKeepAlive, Network: muxcool.NetworkTCP}},
		{"status zero", muxcool.Header{}},
		{"status five", muxcool.Header{Status: 5}},
		{"an option bit the format does not define", muxcool.Header{Status: muxcool.StatusEnd, Options: 0x04}},
		{"a domain over 255 bytes", muxcool.Header{Status: muxcool.StatusNew, Network: muxcool.NetworkTCP, Destination: domainDestination(t, strings.Repeat("a", 256), 80)}},
	} {
		if got, err := muxcool.AppendHeader(nil, tc.header); err == nil {
			t.Errorf("%s: accepted, wrote % x", tc.name, got)
		}
	}
}

func TestADataLengthIsTwoBytesBigEndian(t *testing.T) {
	for length, want := range map[int][]byte{0: {0x00, 0x00}, 1: {0x00, 0x01}, 256: {0x01, 0x00}, 65535: {0xFF, 0xFF}} {
		got, err := muxcool.AppendDataLength(nil, length)
		if err != nil || !bytes.Equal(got, want) {
			t.Errorf("AppendDataLength(%d) = % x, %v; want % x", length, got, err, want)
		}
		read, err := muxcool.ReadDataLength(bytes.NewReader(want))
		if err != nil || read != length {
			t.Errorf("ReadDataLength(% x) = %d, %v; want %d", want, read, err, length)
		}
	}
	for _, length := range []int{-1, 65536} {
		if _, err := muxcool.AppendDataLength(nil, length); err == nil {
			t.Errorf("AppendDataLength(%d) was accepted", length)
		}
	}
	if _, err := muxcool.ReadDataLength(bytes.NewReader([]byte{0x01})); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("one byte: error = %v, want io.ErrUnexpectedEOF", err)
	}
	if _, err := muxcool.ReadDataLength(bytes.NewReader(nil)); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("no bytes: error = %v, want io.ErrUnexpectedEOF, because the metadata was just read", err)
	}
}

func TestAWholeFrameIsReadHeaderThenDataLengthThenData(t *testing.T) {
	header := muxcool.Header{SubConnectionID: 7, Status: muxcool.StatusNew, Options: muxcool.OptionDataFollows, Network: muxcool.NetworkTCP, Destination: domainDestination(t, "example.com", 80)}
	frame, err := muxcool.AppendHeader(nil, header)
	if err != nil {
		t.Fatal(err)
	}
	if frame, err = muxcool.AppendDataLength(frame, 5); err != nil {
		t.Fatal(err)
	}
	frame = append(frame, "hello"...)
	frame = append(frame, "next"...)

	reader := bytes.NewReader(frame)
	got, err := muxcool.ReadHeader(reader)
	if err != nil || got != header {
		t.Fatalf("ReadHeader = %+v, %v", got, err)
	}
	length, err := muxcool.ReadDataLength(reader)
	if err != nil || length != 5 {
		t.Fatalf("ReadDataLength = %d, %v", length, err)
	}
	data := make([]byte, length)
	if _, err := io.ReadFull(reader, data); err != nil || string(data) != "hello" {
		t.Fatalf("data = %q, %v", data, err)
	}
	if rest, _ := io.ReadAll(reader); string(rest) != "next" {
		t.Errorf("left %q, want the next frame untouched", rest)
	}
}

func TestStatusAndNetworkNamesReadInLogs(t *testing.T) {
	if muxcool.StatusNew.String() != "New" || muxcool.StatusKeepAlive.String() != "KeepAlive" || muxcool.Status(9).String() != "Status(9)" {
		t.Error("Status.String")
	}
	if muxcool.NetworkTCP.String() != "TCP" || muxcool.NetworkUDP.String() != "UDP" || muxcool.Network(9).String() != "Network(9)" {
		t.Error("Network.String")
	}
}

func BenchmarkAppendHeaderNewDomain(b *testing.B) {
	header := muxcool.Header{SubConnectionID: 1, Status: muxcool.StatusNew, Network: muxcool.NetworkTCP, Destination: domainDestination(b, "www.example.com", 80)}
	buffer := make([]byte, 0, 64)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := muxcool.AppendHeader(buffer[:0], header); err != nil {
			b.Fatal(err)
		}
	}
}

func benchmarkReadHeader(b *testing.B, header muxcool.Header, buffered bool) {
	wire, err := muxcool.AppendHeader(nil, header)
	if err != nil {
		b.Fatal(err)
	}
	source := bytes.NewReader(wire)
	bufferedReader := bufio.NewReaderSize(source, 4096)
	b.ReportAllocs()
	for b.Loop() {
		source.Reset(wire)
		var err error
		if buffered {
			bufferedReader.Reset(source)
			_, err = muxcool.ReadHeader(bufferedReader)
		} else {
			_, err = muxcool.ReadHeader(source)
		}
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkReadHeaderKeep(b *testing.B) {
	keep := muxcool.Header{SubConnectionID: 1, Status: muxcool.StatusKeep, Options: muxcool.OptionDataFollows}
	b.Run("plain reader", func(b *testing.B) { benchmarkReadHeader(b, keep, false) })
	b.Run("buffered reader", func(b *testing.B) { benchmarkReadHeader(b, keep, true) })
}

func BenchmarkReadHeaderNewDomain(b *testing.B) {
	newDomain := muxcool.Header{SubConnectionID: 1, Status: muxcool.StatusNew, Network: muxcool.NetworkTCP, Destination: domainDestination(b, "www.example.com", 80)}
	b.Run("plain reader", func(b *testing.B) { benchmarkReadHeader(b, newDomain, false) })
	b.Run("buffered reader", func(b *testing.B) { benchmarkReadHeader(b, newDomain, true) })
}
