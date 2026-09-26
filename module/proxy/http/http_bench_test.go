package http_test

import (
	"context"
	"io"
	"net"
	"testing"

	proxyhttp "github.com/The-Apranik/Apranik/module/proxy/http"
)

func BenchmarkAcceptConnect(b *testing.B) {
	server, err := (&proxyhttp.Proxy{}).NewStreamServer()
	if err != nil {
		b.Fatal(err)
	}
	request := []byte("CONNECT example.com:443 HTTP/1.1\r\nHost: example.com:443\r\n\r\n")
	b.ReportAllocs()
	for b.Loop() {
		clientSide, serverSide := net.Pipe()
		go func() {
			clientSide.Write(request)
			io.Copy(io.Discard, clientSide)
		}()
		if _, _, err := server.AcceptStream(context.Background(), pipeStream{serverSide}); err != nil {
			b.Fatal(err)
		}
		serverSide.Close()
		clientSide.Close()
	}
}
