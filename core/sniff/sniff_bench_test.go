package sniff_test

import (
	"crypto/tls"
	"io"
	"net"
	"testing"

	"github.com/The-Apranik/Apranik/core/sniff"
	tlssniffer "github.com/The-Apranik/Apranik/module/sniffer/tls"
	"github.com/The-Apranik/Apranik/spec"
)

// A real ClientHello, captured once from crypto/tls.
func clientHello(b *testing.B) []byte {
	b.Helper()
	clientSide, serverSide := net.Pipe()
	go tls.Client(clientSide, &tls.Config{ServerName: "example.com"}).Handshake()
	header := make([]byte, 5)
	if _, err := io.ReadFull(serverSide, header); err != nil {
		b.Fatal(err)
	}
	body := make([]byte, int(header[3])<<8|int(header[4]))
	if _, err := io.ReadFull(serverSide, body); err != nil {
		b.Fatal(err)
	}
	clientSide.Close()
	serverSide.Close()
	return append(header, body...)
}

func BenchmarkSniffTLSClientHello(b *testing.B) {
	hello := clientHello(b)
	sniffers := []spec.Sniffer{tlssniffer.NewSnifferModule()}
	b.ReportAllocs()
	for b.Loop() {
		clientSide, inboundSide := net.Pipe()
		go clientSide.Write(hello)
		_, result := sniff.Stream(pipeStream{inboundSide}, sniffers, 0)
		if result == nil || result.Domain != "example.com" {
			b.Fatalf("result = %+v", result)
		}
		clientSide.Close()
		inboundSide.Close()
	}
}
