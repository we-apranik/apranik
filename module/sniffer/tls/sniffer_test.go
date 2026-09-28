package tls_test

import (
	cryptotls "crypto/tls"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/The-Apranik/Apranik/module/sniffer/tls"
	"github.com/The-Apranik/Apranik/spec"
)

func newSniffer(t *testing.T) spec.Sniffer {
	t.Helper()
	sniffer, err := tls.NewSnifferModule().NewSniffer()
	if err != nil {
		t.Fatalf("NewSniffer: %v", err)
	}
	return sniffer
}

func clientHelloFor(t *testing.T, serverName string) []byte {
	t.Helper()

	clientSide, serverSide := net.Pipe()
	defer serverSide.Close()

	go func() {
		client := cryptotls.Client(clientSide, &cryptotls.Config{ServerName: serverName, InsecureSkipVerify: serverName == ""})
		client.Handshake()
		clientSide.Close()
	}()

	serverSide.SetReadDeadline(time.Now().Add(2 * time.Second))
	header := make([]byte, 5)
	if _, err := io.ReadFull(serverSide, header); err != nil {
		t.Fatalf("reading record header: %v", err)
	}
	body := make([]byte, int(header[3])<<8|int(header[4]))
	if _, err := io.ReadFull(serverSide, body); err != nil {
		t.Fatalf("reading record body: %v", err)
	}
	return append(header, body...)
}

func TestTheServerNameIsReported(t *testing.T) {
	result, err := newSniffer(t).Sniff(clientHelloFor(t, "Example.COM"))
	if err != nil {
		t.Fatalf("Sniff: %v", err)
	}
	if result.Protocol != "tls" || result.Domain != "Example.COM" {
		t.Errorf("got %+v, want tls Example.COM, unchanged", result)
	}
}

func TestAClientHelloWithoutAServerNameIsStillTLS(t *testing.T) {
	result, err := newSniffer(t).Sniff(clientHelloFor(t, ""))
	if err != nil {
		t.Fatalf("Sniff: %v", err)
	}
	if result.Protocol != "tls" || result.Domain != "" {
		t.Errorf("got %+v, want tls with no domain", result)
	}
}

func TestEveryPrefixAsksForMoreData(t *testing.T) {
	hello := clientHelloFor(t, "example.com")
	sniffer := newSniffer(t)

	for size := 0; size < len(hello); size++ {
		if _, err := sniffer.Sniff(hello[:size]); !errors.Is(err, spec.ErrSniffNeedMoreData) {
			t.Fatalf("a %d-byte prefix of %d returned %v, want ErrSniffNeedMoreData", size, len(hello), err)
		}
	}
}

func TestAClientHelloSplitAcrossRecordsIsReassembled(t *testing.T) {
	hello := clientHelloFor(t, "example.com")
	body := hello[5:]
	half := len(body) / 2

	split := append([]byte{0x16, 0x03, 0x01, byte(half >> 8), byte(half)}, body[:half]...)
	rest := len(body) - half
	split = append(split, 0x16, 0x03, 0x01, byte(rest>>8), byte(rest))
	split = append(split, body[half:]...)

	result, err := newSniffer(t).Sniff(split)
	if err != nil || result.Domain != "example.com" {
		t.Errorf("got %+v, %v; want example.com", result, err)
	}
}

func TestOtherProtocolsAreRefusedAtOnce(t *testing.T) {
	for _, payloadPrefix := range []string{"G", "GET / HTTP/1.1\r\n", "SSH-2.0-OpenSSH_9.6\r\n"} {
		if _, err := newSniffer(t).Sniff([]byte(payloadPrefix)); err == nil || errors.Is(err, spec.ErrSniffNeedMoreData) {
			t.Errorf("%q returned %v, want a refusal", payloadPrefix, err)
		}
	}
}
