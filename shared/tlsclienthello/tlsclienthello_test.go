package tlsclienthello_test

import (
	cryptotls "crypto/tls"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/we-apranik/apranik/shared/tlsclienthello"
	"github.com/we-apranik/apranik/spec"
)

func clientHelloRecord(t *testing.T, serverName string) []byte {
	t.Helper()
	clientSide, serverSide := net.Pipe()
	defer serverSide.Close()
	go func() {
		client := cryptotls.Client(clientSide, &cryptotls.Config{ServerName: serverName, InsecureSkipVerify: serverName == ""})
		client.Handshake()
		clientSide.Close()
	}()
	serverSide.SetReadDeadline(time.Now().Add(2 * time.Second))
	header := make([]byte, tlsclienthello.RecordHeaderSize)
	if _, err := io.ReadFull(serverSide, header); err != nil {
		t.Fatalf("reading record header: %v", err)
	}
	body := make([]byte, tlsclienthello.RecordBodySize(header))
	if _, err := io.ReadFull(serverSide, body); err != nil {
		t.Fatalf("reading record body: %v", err)
	}
	return append(header, body...)
}

func TestTheOffsetPointsAtTheName(t *testing.T) {
	record := clientHelloRecord(t, "blocked.example.com")
	body, err := tlsclienthello.Reassemble(record)
	if err != nil {
		t.Fatalf("Reassemble: %v", err)
	}
	serverName, err := tlsclienthello.FindServerName(body)
	if err != nil {
		t.Fatalf("FindServerName: %v", err)
	}
	if serverName.Name != "blocked.example.com" {
		t.Fatalf("name %q", serverName.Name)
	}
	if got := string(body[serverName.Offset : serverName.Offset+len(serverName.Name)]); got != serverName.Name {
		t.Errorf("bytes at offset %d are %q", serverName.Offset, got)
	}
	inRecord := tlsclienthello.RecordHeaderSize + tlsclienthello.HandshakeHeaderSize + serverName.Offset
	if got := string(record[inRecord : inRecord+len(serverName.Name)]); got != serverName.Name {
		t.Errorf("bytes at record offset %d are %q", inRecord, got)
	}
}

func TestAClientHelloSplitAcrossRecordsIsJoined(t *testing.T) {
	record := clientHelloRecord(t, "example.com")
	header, body := record[:tlsclienthello.RecordHeaderSize], record[tlsclienthello.RecordHeaderSize:]
	var split []byte
	for start := 0; start < len(body); start += 7 {
		piece := body[start:min(start+7, len(body))]
		split = append(split, header[0], header[1], header[2], byte(len(piece)>>8), byte(len(piece)))
		split = append(split, piece...)
	}
	joined, err := tlsclienthello.Reassemble(split)
	if err != nil {
		t.Fatalf("Reassemble: %v", err)
	}
	serverName, err := tlsclienthello.FindServerName(joined)
	if err != nil || serverName.Name != "example.com" {
		t.Errorf("got %+v, %v", serverName, err)
	}
}

func TestNoServerNameIsNotAnError(t *testing.T) {
	body, err := tlsclienthello.Reassemble(clientHelloRecord(t, ""))
	if err != nil {
		t.Fatalf("Reassemble: %v", err)
	}
	serverName, err := tlsclienthello.FindServerName(body)
	if err != nil || serverName != (tlsclienthello.ServerName{}) {
		t.Errorf("got %+v, %v", serverName, err)
	}
}

func TestAShortStartAsksForMore(t *testing.T) {
	record := clientHelloRecord(t, "example.com")
	if _, err := tlsclienthello.Reassemble(record[:40]); !errors.Is(err, spec.ErrSniffNeedMoreData) {
		t.Errorf("got %v, want ErrSniffNeedMoreData", err)
	}
}

func TestOtherBytesAreNotAClientHello(t *testing.T) {
	if _, err := tlsclienthello.Reassemble([]byte("GET / HTTP/1.1\r\n\r\n")); !errors.Is(err, tlsclienthello.ErrNotClientHello) {
		t.Errorf("got %v, want ErrNotClientHello", err)
	}
}

func TestExtensionTypesListsEveryExtensionInOrder(t *testing.T) {
	body, err := tlsclienthello.Reassemble(clientHelloRecord(t, "example.com"))
	if err != nil {
		t.Fatalf("Reassemble: %v", err)
	}
	extensionTypes, err := tlsclienthello.ExtensionTypes(body)
	if err != nil {
		t.Fatalf("ExtensionTypes: %v", err)
	}
	const serverName, supportedVersions = 0x0000, 0x002b
	if len(extensionTypes) == 0 || extensionTypes[0] != serverName {
		t.Errorf("extensions = %#x, want server_name first, as Go sends it", extensionTypes)
	}
	found := false
	for _, extensionType := range extensionTypes {
		found = found || extensionType == supportedVersions
	}
	if !found {
		t.Errorf("extensions = %#x, want supported_versions among them", extensionTypes)
	}
}

func TestExtensionTypesRefusesATruncatedClientHello(t *testing.T) {
	body, err := tlsclienthello.Reassemble(clientHelloRecord(t, "example.com"))
	if err != nil {
		t.Fatalf("Reassemble: %v", err)
	}
	if _, err := tlsclienthello.ExtensionTypes(body[:len(body)-3]); !errors.Is(err, tlsclienthello.ErrMalformed) {
		t.Errorf("err = %v, want ErrMalformed", err)
	}
}

func TestIsGREASE(t *testing.T) {
	for _, value := range []uint16{0x0a0a, 0x1a1a, 0x5a5a, 0xfafa} {
		if !tlsclienthello.IsGREASE(value) {
			t.Errorf("%#04x is GREASE", value)
		}
	}
	for _, value := range []uint16{0x0000, 0x0a1a, 0x1a0a, 0x4469, 0x0a0b, 0xfe0d} {
		if tlsclienthello.IsGREASE(value) {
			t.Errorf("%#04x is not GREASE", value)
		}
	}
}
